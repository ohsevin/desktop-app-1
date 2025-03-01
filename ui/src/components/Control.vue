<template>
  <div class="flexColumn">
    <transition name="fade-super-quick" mode="out-in">
      <div
        class="flexColumn"
        v-if="uiView === 'serversEntry'"
        key="entryServers"
      >
        <Servers
          :onBack="backToMainView"
          :onServerChanged="onServerChanged"
          :onFastestServer="onFastestServer"
          :onRandomServer="onRandomServer"
        />
      </div>

      <div
        class="flexColumn"
        v-else-if="uiView === 'serversExit'"
        key="exitServers"
      >
        <Servers
          :onBack="backToMainView"
          isExitServer="true"
          :onServerChanged="onServerChanged"
          :onRandomServer="() => onRandomServer(true)"
        />
      </div>

      <div v-else class="flexColumn">
        <div>
          <ConnectBlock
            :onChecked="switchChecked"
            :isChecked="isConnected"
            :isProgress="isInProgress"
            :onPauseResume="onPauseResume"
            :pauseState="this.$store.state.vpnState.pauseState"
          />
          <div class="horizontalLine hopButtonsSeparator" />
        </div>

        <div
          ref="scrollArea"
          class="scrollableColumnContainer"
          @scroll="recalcScrollButtonVisiblity()"
        >
          <div v-if="isMultihopAllowed">
            <HopButtonsBlock />
            <div class="horizontalLine hopButtonsSeparator" />
          </div>

          <SelectedServerBlock :onShowServersPressed="onShowServersPressed" />

          <div v-if="this.$store.state.settings.isMultiHop">
            <div class="horizontalLine" />
            <SelectedServerBlock
              :onShowServersPressed="onShowServersPressed"
              isExitServer="true"
            />
          </div>

          <ConnectionDetailsBlock
            :onShowPorts="onShowPorts"
            :onShowWifiConfig="onShowWifiConfig"
          />

          <transition name="fade">
            <button
              class="btnScrollDown"
              v-if="isShowScrollButton"
              v-on:click="onScrollDown()"
            >
              <img src="@/assets/arrow-bottom.svg" />
            </button>
          </transition>
        </div>
      </div>
    </transition>
  </div>
</template>

<script>
import Servers from "./Servers.vue";
import ConnectBlock from "./blocks/block-connect.vue";
import ConnectionDetailsBlock from "./blocks/block-connection-details.vue";
import SelectedServerBlock from "@/components/blocks/block-selected-server.vue";
import HopButtonsBlock from "./blocks/block-hop-buttons.vue";

const sender = window.ipcSender;
import { VpnStateEnum, VpnTypeEnum, PauseStateEnum } from "@/store/types";
import { isStrNullOrEmpty } from "@/helpers/helpers";

const viewTypeEnum = Object.freeze({
  default: "default",
  serversEntry: "serversEntry",
  serversExit: "serversExit",
});

async function connect(me, isConnect) {
  try {
    me.isConnectProgress = true;
    if (isConnect === true) await sender.Connect();
    else await sender.Disconnect();
  } catch (e) {
    console.error(e);
    sender.showMessageBoxSync({
      type: "error",
      buttons: ["OK"],
      message: `Failed to ${isConnect ? "connect" : "disconnect"}: ` + e,
    });
  } finally {
    me.isConnectProgress = false;
  }
}

function connected(me) {
  return me.$store.state.vpnState.connectionState === VpnStateEnum.CONNECTED;
}

export default {
  props: {
    onConnectionSettings: Function,
    onWifiSettings: Function,
    onDefaultView: Function,
  },

  components: {
    HopButtonsBlock,
    Servers,
    ConnectBlock,
    SelectedServerBlock,
    ConnectionDetailsBlock,
  },
  mounted() {
    this.recalcScrollButtonVisiblity();

    // ResizeObserver sometimes is stopping to work for unknown reason. So, We do not use it for now
    // Instead, watchers are in use: isMinimizedUI, isMultiHop
    //const resizeObserver = new ResizeObserver(this.recalcScrollButtonVisiblity);
    //resizeObserver.observe(this.$refs.scrollArea);
  },
  data: function () {
    return {
      isShowScrollButton: false,
      isConnectProgress: false,
      uiView: viewTypeEnum.default,
      lastServersPingRequestTime: null,
    };
  },

  computed: {
    isConnected: function () {
      return connected(this);
    },
    isOpenVPN: function () {
      return this.$store.state.settings.vpnType === VpnTypeEnum.OpenVPN;
    },
    isMultihopAllowed: function () {
      return this.$store.getters["account/isMultihopAllowed"];
    },
    isInProgress: function () {
      if (this.isConnectProgress) return this.isConnectProgress;
      return (
        this.$store.state.vpnState.connectionState !== VpnStateEnum.CONNECTED &&
        this.$store.state.vpnState.connectionState !== VpnStateEnum.DISCONNECTED
      );
    },
    // needed for watcher
    conectionState: function () {
      return this.$store.state.vpnState.connectionState;
    },
    isMinimizedUI: function () {
      return this.$store.state.settings.minimizedUI;
    },
    isMultiHop: function () {
      return this.$store.state.settings.isMultiHop;
    },
  },

  watch: {
    conectionState(newValue, oldValue) {
      // show connection failure description:

      // only in case of changing to DISCONNECTED
      if (newValue !== VpnStateEnum.DISCONNECTED || newValue == oldValue)
        return;

      // if disconnection reason defined
      let failureInfo = this.$store.state.vpnState.disconnectedInfo;
      if (!failureInfo || isStrNullOrEmpty(failureInfo.ReasonDescription))
        return;

      sender.showMessageBoxSync({
        type: "error",
        buttons: ["OK"],
        message: `Failed to connect`,
        detail: failureInfo.ReasonDescription,
      });
    },
    isMinimizedUI() {
      setTimeout(() => this.recalcScrollButtonVisiblity(), 1000);
    },
    isMultiHop() {
      setTimeout(() => this.recalcScrollButtonVisiblity(), 1000);
    },
  },

  methods: {
    async switchChecked(isConnect) {
      connect(this, isConnect);
    },
    async onPauseResume(seconds) {
      if (seconds == null || seconds == 0) {
        // RESUME
        if (this.$store.state.vpnState.pauseState != PauseStateEnum.Resumed)
          await sender.ResumeConnection();
      } else {
        // PAUSE
        await sender.PauseConnection(seconds);
      }
    },
    onShowServersPressed(isExitServers) {
      this.uiView = isExitServers
        ? viewTypeEnum.serversExit
        : viewTypeEnum.serversEntry;

      if (this.onDefaultView) this.onDefaultView(false);

      // request servers ping not more often than once per 30 seconds
      if (
        this.lastServersPingRequestTime == null ||
        (new Date().getTime() - this.lastServersPingRequestTime.getTime()) /
          1000 >
          30
      ) {
        sender.PingServers();
        this.lastServersPingRequestTime = new Date();
      }
    },
    onShowPorts() {
      if (this.onConnectionSettings != null) this.onConnectionSettings();
    },
    onShowWifiConfig() {
      if (this.onWifiSettings != null) this.onWifiSettings();
    },
    backToMainView() {
      this.uiView = viewTypeEnum.default;
      if (this.onDefaultView) this.onDefaultView(true);

      setTimeout(this.recalcScrollButtonVisiblity, 1000);
    },
    onServerChanged(server, isExitServer) {
      if (server == null || isExitServer == null) return;

      let needReconnect = false;
      if (!isExitServer) {
        if (
          !this.$store.state.settings.serverEntry ||
          this.$store.state.settings.serverEntry.gateway !== server.gateway ||
          this.$store.state.settings.isRandomServer !== false
        ) {
          this.$store.dispatch("settings/isRandomServer", false);
          this.$store.dispatch("settings/serverEntry", server);
          needReconnect = true;
        }
      } else {
        if (
          !this.$store.state.settings.serverExit ||
          this.$store.state.settings.serverExit.gateway !== server.gateway ||
          this.$store.state.settings.isRandomExitServer !== false
        ) {
          this.$store.dispatch("settings/isRandomExitServer", false);
          this.$store.dispatch("settings/serverExit", server);
          needReconnect = true;
        }
      }
      if (this.$store.state.settings.isFastestServer !== false) {
        this.$store.dispatch("settings/isFastestServer", false);
        needReconnect = true;
      }

      if (needReconnect == true && connected(this)) connect(this, true);
    },
    onFastestServer() {
      this.$store.dispatch("settings/isFastestServer", true);
      if (connected(this)) connect(this, true);
    },
    onRandomServer(isExitServer) {
      if (isExitServer === true)
        this.$store.dispatch("settings/isRandomExitServer", true);
      else this.$store.dispatch("settings/isRandomServer", true);
      if (connected(this)) connect(this, true);
    },
    recalcScrollButtonVisiblity() {
      let sa = this.$refs.scrollArea;
      if (sa == null) {
        this.isShowScrollButton = false;
        return;
      }

      const isNeedToShow = function () {
        let pixelsToTheEndScroll =
          sa.scrollHeight - (sa.clientHeight + sa.scrollTop);
        // hide if the 'pixels to scroll' < 20
        if (pixelsToTheEndScroll < 20) return false;
        return true;
      };

      // hide - imediately; show - with 1sec delay
      if (!isNeedToShow()) this.isShowScrollButton = false;
      else {
        setTimeout(() => {
          this.isShowScrollButton = isNeedToShow();
        }, 1000);
      }
    },
    onScrollDown() {
      let sa = this.$refs.scrollArea;
      if (sa == null) return;
      sa.scrollTo({
        top: sa.scrollHeight,
        behavior: "smooth",
      });
    },
  },
};
</script>

<!-- Add "scoped" attribute to limit CSS to this component only -->
<style scoped lang="scss">
@import "@/components/scss/constants";
</style>
