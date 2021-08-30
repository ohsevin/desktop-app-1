//+build windows

package oshelpers

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ivpn/desktop-app/daemon/service/platform"
	lnk "github.com/parsiya/golnk"
)

func WinExpandEnvPath(path string) string {
	// match windows-style variables. E.g.: %windir%
	re := regexp.MustCompile("%[^%]+%")
	path = re.ReplaceAllStringFunc(path, func(str string) string {
		return "${" + strings.Trim(str, "%") + "}"
	})
	return os.ExpandEnv(path)
}

func implGetInstalledApps() ([]AppInfo, error) {

	programData := os.Getenv("PROGRAMDATA")
	appData := os.Getenv("APPDATA")
	programDataSMDir := ""
	appDataSMDir := ""

	excludeStartMenuPaths := make([]string, 0, 2)
	if len(programData) > 0 {
		programDataSMDir = programData + `\Microsoft\Windows\Start Menu\Programs`
		excludeStartMenuPaths = append(excludeStartMenuPaths, strings.ToLower(programDataSMDir+`\startup`))
		excludeStartMenuPaths = append(excludeStartMenuPaths, strings.ToLower(programDataSMDir+`\Administrative Tools`))
	}
	if len(appData) > 0 {
		appDataSMDir = appData + `\Microsoft\Windows\Start Menu\Programs`
		excludeStartMenuPaths = append(excludeStartMenuPaths, strings.ToLower(appDataSMDir+`\startup`))
		excludeStartMenuPaths = append(excludeStartMenuPaths, strings.ToLower(appDataSMDir+`\Administrative Tools`))
	}

	// ignore all binaries from IVPN installation
	excludeBinPath := ""
	if ex, err := os.Executable(); err == nil && len(ex) > 0 {
		excludeBinPath = strings.ToLower(filepath.Dir(ex))
	}

	retMap := make(map[string]AppInfo) // [path]description

	walkFunc := func(path string, info os.FileInfo, walkErr error) (err error) {

		defer func() {
			if r := recover(); r != nil {
				errText := ""
				if theErr, ok := r.(error); ok {
					errText = fmt.Sprintf("PANIC [recovered] on implGetInstalledApps() for '%s' : %v", path, theErr)
				} else {
					errText = fmt.Sprintf("PANIC [recovered] on implGetInstalledApps() for '%s'", path)
				}
				log.Error(errText)
			}
		}()

		// Only look for lnk files.
		if filepath.Ext(info.Name()) == ".lnk" {

			// ignore files from 'excludePaths'
			for _, excludePath := range excludeStartMenuPaths {
				curLnkPath := strings.ToLower(path)
				if strings.HasPrefix(curLnkPath, excludePath) {
					return nil
				}
			}

			f, lnkErr := lnk.File(path)

			if lnkErr != nil {
				return nil
			}

			var targetPath = ""
			if f.LinkInfo.LocalBasePath != "" {
				targetPath = f.LinkInfo.LocalBasePath
			}
			if f.LinkInfo.LocalBasePathUnicode != "" {
				targetPath = f.LinkInfo.LocalBasePathUnicode
			}
			if f.StringData.IconLocation != "" {
				targetPath = f.StringData.IconLocation
			}

			if targetPath == "" {
				return
			}

			// expand all environment variables in file path
			targetPath = WinExpandEnvPath(targetPath)

			if _, isAlreadyExists := retMap[targetPath]; isAlreadyExists {
				return
			}

			// Only look for exe files.
			if targetPath != "" && filepath.Ext(targetPath) == ".exe" {
				baseDir := filepath.Dir(path)

				if strings.EqualFold(baseDir, programDataSMDir) || strings.EqualFold(baseDir, appDataSMDir) {
					baseDir = ""
				} else {
					baseDir = filepath.Base(baseDir)
				}

				if _, err := os.Stat(targetPath); os.IsNotExist(err) {
					// file not exists
					return nil
				}

				// ignore all binaries from IVPN installation
				if strings.HasPrefix(strings.ToLower(targetPath), excludeBinPath) {
					return nil
				}

				retMap[targetPath] = AppInfo{
					AppBinaryPath: targetPath,
					AppName:       strings.TrimSuffix(info.Name(), ".lnk"),
					AppGroup:      baseDir}

			}
		}

		return nil
	}

	if len(programDataSMDir) > 0 {
		filepath.Walk(programDataSMDir, walkFunc)
	}

	if len(appDataSMDir) > 0 {
		filepath.Walk(appDataSMDir, walkFunc)
	}

	retValues := make([]AppInfo, 0, len(retMap))
	for _, value := range retMap {
		retValues = append(retValues, value)
	}

	// extract icons from binaries

	binaryIconReaderInit()
	defer binaryIconReaderUnInit()
	for i, app := range retValues {
		ico, err := binaryIconReaderGetBase64PngIcon(app.AppBinaryPath)
		if err != nil {
			log.Warning(err)
		} else {
			retValues[i].AppIcon = ico
		}
	}

	// sort by app name
	sort.Slice(retValues[:], func(i, j int) bool {
		return strings.Compare(retValues[i].AppName, retValues[j].AppName) == -1
	})

	return retValues, nil
}

func implGetBinaryIconBase64Png(binaryPath string) (icon string, err error) {
	binaryIconReaderInit()
	defer binaryIconReaderUnInit()
	return binaryIconReaderGetBase64PngIcon(binaryPath)
}

// =============================================================================
// ============= Internal implementation =======================================
// =============================================================================
var (
	_fBinaryIconReaderInit          *syscall.LazyProc // DWORD _cdecl BinaryIconReaderInit()
	_fBinaryIconReaderUnInit        *syscall.LazyProc // DWORD _cdecl BinaryIconReaderUnInit()
	_fBinaryIconReaderReadBase64Png *syscall.LazyProc // DWORD _cdecl BinaryIconReaderReadBase64Png(const wchar_t* binaryPath, unsigned char* buff, DWORD* _in_out_buffSize)
)

var (
	_iconReaderInitCounter      int
	_iconReaderInitCounterMutex sync.Mutex
)

func initBinaryIconReaderDll() error {
	if _fBinaryIconReaderInit != nil {
		return nil
	}
	helpersDllPath := platform.WindowsNativeHelpersDllPath()
	if len(helpersDllPath) == 0 {
		return fmt.Errorf("unable to BinaryIconReader: helpers dll path not initialized")
	}
	if _, err := os.Stat(helpersDllPath); err != nil {
		return fmt.Errorf("unable to BinaryIconReader (helpers dll not found) : '%s'", helpersDllPath)
	}

	dll := syscall.NewLazyDLL(helpersDllPath)
	_fBinaryIconReaderInit = dll.NewProc("BinaryIconReaderInit")
	_fBinaryIconReaderUnInit = dll.NewProc("BinaryIconReaderUnInit")
	_fBinaryIconReaderReadBase64Png = dll.NewProc("BinaryIconReaderReadBase64Png")
	return nil
}

func checkCallErrResp(retval uintptr, err error, mName string) error {
	if err != syscall.Errno(0) {
		return log.ErrorE(fmt.Errorf("%s:  %w", mName, err), 1)
	}
	if retval != 1 {
		return log.ErrorE(fmt.Errorf("BinaryIconReader operation failed (%s)", mName), 1)
	}
	return nil
}

func binaryIconReaderInit() error {

	// Calculate how many process using this functionality
	// Call '_fBinaryIconReaderInit' only once and '_fBinaryIconReaderUnInit' only when nobody using this functionality
	// NOTE! every call 'binaryIconReaderInit()' should be finished by 'binaryIconReaderUnInit()'
	_iconReaderInitCounterMutex.Lock()
	defer _iconReaderInitCounterMutex.Unlock()
	_iconReaderInitCounter += 1
	if _iconReaderInitCounter > 1 {
		return nil
	}

	if err := initBinaryIconReaderDll(); err != nil {
		return err
	}

	retval, _, err := _fBinaryIconReaderInit.Call()
	if err := checkCallErrResp(retval, err, "BinaryIconReaderInit"); err != nil {
		return err
	}
	return nil
}

func binaryIconReaderUnInit() error {
	// Calculate how many process using this functionality
	// Call '_fBinaryIconReaderInit' only once and '_fBinaryIconReaderUnInit' only when nobody using this functionality
	_iconReaderInitCounterMutex.Lock()
	defer _iconReaderInitCounterMutex.Unlock()
	_iconReaderInitCounter -= 1
	if _iconReaderInitCounter > 0 {
		return nil
	}
	_iconReaderInitCounter = 0

	if err := initBinaryIconReaderDll(); err != nil {
		return err
	}

	retval, _, err := _fBinaryIconReaderUnInit.Call()
	if err := checkCallErrResp(retval, err, "BinaryIconReaderUnInit"); err != nil {
		return err
	}
	return nil
}

func binaryIconReaderGetBase64PngIcon(binaryPath string) (icon string, err error) {
	if err := initBinaryIconReaderDll(); err != nil {
		return "", err
	}

	utfBinaryPath, err := syscall.UTF16PtrFromString(binaryPath)
	if err != nil {
		return "", fmt.Errorf("(implBinaryIconReaderGetBase64PngIcon) Failed to convert binaryPath: %w", err)
	}
	var (
		iconReaderBuffSize uint32 = 1024 * 5
		iconReaderBuff     []byte = make([]byte, iconReaderBuffSize)
	)

	buffSize := iconReaderBuffSize

	retval, _, err := _fBinaryIconReaderReadBase64Png.Call(uintptr(unsafe.Pointer(utfBinaryPath)),
		uintptr(unsafe.Pointer(&iconReaderBuff[0])),
		uintptr(unsafe.Pointer(&buffSize)))

	if retval != 1 && buffSize > iconReaderBuffSize && buffSize < 1024*15 {
		iconReaderBuffSize = buffSize
		iconReaderBuff = make([]byte, iconReaderBuffSize)

		retval, _, err = _fBinaryIconReaderReadBase64Png.Call(uintptr(unsafe.Pointer(utfBinaryPath)),
			uintptr(unsafe.Pointer(&iconReaderBuff[0])),
			uintptr(unsafe.Pointer(&buffSize)))
	}

	if err := checkCallErrResp(retval, err, "BinaryIconReaderReadBase64Png"); err != nil {
		return "", err
	}

	return string(iconReaderBuff[:buffSize]), nil
}
