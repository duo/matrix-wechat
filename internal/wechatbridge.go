package internal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/duo/matrix-wechat/internal/config"
	"github.com/duo/matrix-wechat/internal/database"
	"github.com/duo/matrix-wechat/internal/types"
	"github.com/duo/matrix-wechat/internal/wechat"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/bridge/commands"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"
)

type WechatBridge struct {
	bridge.Bridge
	Config        *config.Config
	DB            *database.Database
	Formatter     *Formatter
	WechatService *wechat.WechatService
	ExampleConfig string

	usersByMXID         map[id.UserID]*User
	usersByUsername     map[string]*User
	usersLock           sync.Mutex
	managementRooms     map[id.RoomID]*User
	portalsByMXID       map[id.RoomID]*Portal
	portalsByUID        map[database.PortalKey]*Portal
	portalsLock         sync.Mutex
	puppets             map[types.UID]*Puppet
	puppetsByCustomMXID map[id.UserID]*Puppet
	puppetsLock         sync.Mutex
	checkers            map[id.UserID]chan struct{}
	checkersLock        sync.Mutex

	WebsocketHandler *WebsocketCommandHandler

	stopping   bool
	stopPinger chan struct{}

	shortCircuitReconnectBackoff chan struct{}
	websocketStarted             chan struct{}
	websocketStopped             chan struct{}
}

func NewWechatBridge(exampleConfig string) *WechatBridge {
	return &WechatBridge{
		ExampleConfig:       exampleConfig,
		usersByMXID:         make(map[id.UserID]*User),
		usersByUsername:     make(map[string]*User),
		managementRooms:     make(map[id.RoomID]*User),
		portalsByMXID:       make(map[id.RoomID]*Portal),
		portalsByUID:        make(map[database.PortalKey]*Portal),
		puppets:             make(map[types.UID]*Puppet),
		puppetsByCustomMXID: make(map[id.UserID]*Puppet),
		checkers:            make(map[id.UserID]chan struct{}),

		shortCircuitReconnectBackoff: make(chan struct{}),
		websocketStarted:             make(chan struct{}),
		websocketStopped:             make(chan struct{}),
	}
}

func (br *WechatBridge) GetExampleConfig() string {
	return br.ExampleConfig
}

func (br *WechatBridge) GetConfigPtr() interface{} {
	br.Config = &config.Config{
		BaseConfig: &br.Bridge.Config,
	}
	br.Config.BaseConfig.Bridge = &br.Config.Bridge

	return br.Config
}

func (br *WechatBridge) Init() {
	br.CommandProcessor = commands.NewProcessor(&br.Bridge)
	br.RegisterCommands()

	br.EventProcessor.On(event.EphemeralEventPresence, br.HandlePresence)

	br.DB = database.New(br.Bridge.DB, br.Log.Sub("Database"))

	br.Formatter = NewFormatter(br)
	br.WechatService = wechat.NewWechatService(
		br.Config.Bridge.ListenAddress,
		br.Config.Bridge.ListenSecret,
		br.Log,
	)

	br.WebsocketHandler = NewWebsocketCommandHandler(br)
}

func (br *WechatBridge) Start() {
	if br.Config.Homeserver.WSProxy != "" {
		var startupGroup sync.WaitGroup
		startupGroup.Add(1)

		br.Log.Debugln("Starting application service websocket")
		go br.startWebsocket(&startupGroup)

		startupGroup.Wait()

		br.stopPinger = make(chan struct{})
		if br.Config.Homeserver.WSPingInterval > 0 {
			go br.serverPinger()
		}
	} else {
		if br.Config.AppService.Port == 0 {
			br.Log.Fatalln("Both the websocket proxy and appservice listener are disabled, can't receive events")
			os.Exit(23)
		}
		br.Log.Debugln("Websocket proxy not configured, not starting application service websocket")
	}

	go br.WechatService.Start()
	go br.StartUsers()
}

type PingData struct {
	Timestamp int64 `json:"timestamp"`
}

func (br *WechatBridge) PingServer() (start, serverTs, end time.Time) {
	if !br.AS.HasWebsocket() {
		br.Log.Debugln("Received server ping request, but no websocket connected. Trying to short-circuit backoff sleep")
		select {
		case br.shortCircuitReconnectBackoff <- struct{}{}:
		default:
			br.Log.Warnfln("Failed to ping websocket: not connected and no backoff?")
			return
		}
		select {
		case <-br.websocketStarted:
		case <-time.After(15 * time.Second):
			if !br.AS.HasWebsocket() {
				br.Log.Warnfln("Failed to ping websocket: didn't connect after 15 seconds of waiting")
				return
			}
		}
	}
	start = time.Now()
	var resp PingData
	br.Log.Debugln("Pinging appservice websocket")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := br.AS.RequestWebsocket(ctx, &appservice.WebsocketRequest{
		Command: "ping",
		Data:    &PingData{Timestamp: start.UnixMilli()},
	}, &resp)
	end = time.Now()
	if err != nil {
		br.Log.Warnfln("Websocket ping returned error in %s: %v", end.Sub(start), err)
		br.AS.StopWebsocket(fmt.Errorf("websocket ping returned error in %s: %w", end.Sub(start), err))
	} else {
		serverTs = time.Unix(0, resp.Timestamp*int64(time.Millisecond))
		br.Log.Debugfln("Websocket ping returned success in %s (request: %s, response: %s)", end.Sub(start), serverTs.Sub(start), end.Sub(serverTs))
	}
	return
}

func (br *WechatBridge) serverPinger() {
	interval := time.Duration(br.Config.Homeserver.WSPingInterval) * time.Second
	clock := time.NewTicker(interval)
	defer func() {
		br.Log.Infofln("Websocket pinger stopped")
		clock.Stop()
	}()
	br.Log.Infofln("Pinging websocket every %s", interval)
	for {
		select {
		case <-clock.C:
			br.PingServer()
		case <-br.stopPinger:
			return
		}
		if br.stopping {
			return
		}
	}
}

func (br *WechatBridge) Stop() {
	br.checkersLock.Lock()
	for _, checker := range br.checkers {
		select {
		case checker <- struct{}{}:
		default:
		}
	}
	br.checkersLock.Unlock()

	br.usersLock.Lock()
	for _, user := range br.usersByUsername {
		if user.Client == nil {
			continue
		}
		br.Log.Debugln("Disconnecting", user.MXID)
		user.DeleteConnection()
	}
	br.usersLock.Unlock()

	br.WechatService.Stop()

	br.stopping = true

	if br.Config.Homeserver.WSProxy != "" {
		select {
		case br.stopPinger <- struct{}{}:
		default:
		}
		br.Log.Debugln("Stopping transaction websocket")
		br.AS.StopWebsocket(appservice.ErrWebsocketManualStop)
		br.Log.Debugln("Stopping event processor")
		// Short-circuit reconnect backoff so the websocket loop exits even if it's disconnected
		select {
		case br.shortCircuitReconnectBackoff <- struct{}{}:
		default:
		}
		select {
		case <-br.websocketStopped:
		case <-time.After(4 * time.Second):
			br.Log.Warnln("Timed out waiting for websocket to close")
		}
	}
}

func (br *WechatBridge) StartUsers() {
	br.Log.Debugln("Starting custom puppets")
	for _, loopuppet := range br.GetAllPuppetsWithCustomMXID() {
		go func(puppet *Puppet) {
			puppet.log.Debugln("Starting custom puppet", puppet.CustomMXID)
			err := puppet.StartCustomMXID(true)
			if err != nil {
				puppet.log.Errorln("Failed to start custom puppet:", err)
			}
		}(loopuppet)
	}
}

func (br *WechatBridge) CreatePrivatePortal(roomID id.RoomID, brInviter bridge.User, brGhost bridge.Ghost) {
	inviter := brInviter.(*User)
	puppet := brGhost.(*Puppet)
	key := database.NewPortalKey(puppet.UID, inviter.UID)
	portal := br.GetPortalByUID(key)

	if len(portal.MXID) == 0 {
		br.createPrivatePortalFromInvite(roomID, inviter, puppet, portal)
		return
	}

	ok := portal.ensureUserInvited(inviter)
	if !ok {
		br.Log.Warnfln("Failed to invite %s to existing private chat portal %s with %s. Redirecting portal to new room...", inviter.MXID, portal.MXID, puppet.UID)
		br.createPrivatePortalFromInvite(roomID, inviter, puppet, portal)
		return
	}
	intent := puppet.DefaultIntent()
	errorMessage := fmt.Sprintf("You already have a private chat portal with me at [%[1]s](https://matrix.to/#/%[1]s)", portal.MXID)
	errorContent := format.RenderMarkdown(errorMessage, true, false)
	_, _ = intent.SendMessageEvent(roomID, event.EventMessage, errorContent)
	br.Log.Debugfln("Leaving private chat room %s as %s after accepting invite from %s as we already have chat with the user", roomID, puppet.MXID, inviter.MXID)
	_, _ = intent.LeaveRoom(roomID)
}

func (br *WechatBridge) createPrivatePortalFromInvite(roomID id.RoomID, inviter *User, puppet *Puppet, portal *Portal) {
	portal.MXID = roomID
	portal.Topic = PrivateChatTopic
	_, _ = portal.MainIntent().SetRoomTopic(portal.MXID, portal.Topic)
	if portal.bridge.Config.Bridge.PrivateChatPortalMeta {
		portal.Name = puppet.Displayname
		portal.AvatarURL = puppet.AvatarURL
		portal.Avatar = puppet.Avatar
		_, _ = portal.MainIntent().SetRoomName(portal.MXID, portal.Name)
		_, _ = portal.MainIntent().SetRoomAvatar(portal.MXID, portal.AvatarURL)
	} else {
		portal.Name = ""
	}
	portal.log.Infofln("Created private chat portal in %s after invite from %s", roomID, inviter.MXID)
	intent := puppet.DefaultIntent()

	if br.Config.Bridge.Encryption.Default {
		_, err := intent.InviteUser(roomID, &mautrix.ReqInviteUser{UserID: br.Bot.UserID})
		if err != nil {
			portal.log.Warnln("Failed to invite bridge bot to enable e2be:", err)
		}
		err = br.Bot.EnsureJoined(roomID)
		if err != nil {
			portal.log.Warnln("Failed to join as bridge bot to enable e2be:", err)
		}
		_, err = intent.SendStateEvent(roomID, event.StateEncryption, "", portal.GetEncryptionEventContent())
		if err != nil {
			portal.log.Warnln("Failed to enable e2be:", err)
		}
		br.AS.StateStore.SetMembership(roomID, inviter.MXID, event.MembershipJoin)
		br.AS.StateStore.SetMembership(roomID, puppet.MXID, event.MembershipJoin)
		br.AS.StateStore.SetMembership(roomID, br.Bot.UserID, event.MembershipJoin)
		portal.Encrypted = true
	}
	portal.Update(nil)
	portal.UpdateBridgeInfo()
	_, _ = intent.SendNotice(roomID, "Private chat portal created")
}

func (br *WechatBridge) HandlePresence(evt *event.Event) {
	// TODO:
}

const defaultReconnectBackoff = 2 * time.Second
const maxReconnectBackoff = 2 * time.Minute
const reconnectBackoffReset = 5 * time.Minute

type StartSyncRequest struct {
	AccessToken string      `json:"access_token"`
	DeviceID    id.DeviceID `json:"device_id"`
	UserID      id.UserID   `json:"user_id"`
}

func (br *WechatBridge) SendBridgeStatus() {
	state := BridgeStatus{}

	state.StateEvent = BridgeStatusConnected
	state.Timestamp = time.Now().Unix()
	state.TTL = 600
	state.Source = "bridge"
	//state.RemoteID = "unknown"

	if err := br.AS.SendWebsocket(&appservice.WebsocketRequest{
		Command: "bridge_status",
		Data:    &state,
	}); err != nil {
		br.Log.Warnln("Error sending bridge status:", err)
	}
}

func (br *WechatBridge) RequestStartSync() {
	if !br.Config.Bridge.Encryption.Appservice ||
		br.Config.Homeserver.Software == bridgeconfig.SoftwareHungry ||
		br.Crypto == nil ||
		!br.AS.HasWebsocket() {
		return
	}
	resp := map[string]interface{}{}
	br.Log.Debugln("Sending /sync start request through websocket")
	cryptoClient := br.Crypto.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	err := br.AS.RequestWebsocket(ctx, &appservice.WebsocketRequest{
		Command:  "start_sync",
		Deadline: 30 * time.Second,
		Data: &StartSyncRequest{
			AccessToken: cryptoClient.AccessToken,
			DeviceID:    cryptoClient.DeviceID,
			UserID:      cryptoClient.UserID,
		},
	}, &resp)
	if err != nil {
		go br.WebsocketHandler.HandleSyncProxyError(nil, err)
	} else {
		br.Log.Debugln("Started receiving encryption data with sync proxy:", resp)
	}
}

func (br *WechatBridge) startWebsocket(wg *sync.WaitGroup) {
	var wgOnce sync.Once
	onConnect := func() {
		go br.SendBridgeStatus()

		br.RequestStartSync()

		wgOnce.Do(wg.Done)

		select {
		case br.websocketStarted <- struct{}{}:
		default:
		}
	}

	reconnectBackoff := defaultReconnectBackoff
	lastDisconnect := time.Now().UnixNano()
	defer func() {
		br.Log.Debugfln("Appservice websocket loop finished")
		close(br.websocketStopped)
	}()

	for {
		err := br.AS.StartWebsocket(br.Config.Homeserver.WSProxy, onConnect)
		if err == appservice.ErrWebsocketManualStop {
			return
		} else if closeCommand := (&appservice.CloseCommand{}); errors.As(err, &closeCommand) && closeCommand.Status == appservice.MeowConnectionReplaced {
			br.Log.Infoln("Appservice websocket closed by another instance of the bridge, shutting down...")
			br.Stop()
			return
		} else if err != nil {
			br.Log.Errorln("Error in appservice websocket:", err)
		}
		if br.stopping {
			return
		}
		now := time.Now().UnixNano()
		if lastDisconnect+reconnectBackoffReset.Nanoseconds() < now {
			reconnectBackoff = defaultReconnectBackoff
		} else {
			reconnectBackoff *= 2
			if reconnectBackoff > maxReconnectBackoff {
				reconnectBackoff = maxReconnectBackoff
			}
		}
		lastDisconnect = now
		br.Log.Infofln("Websocket disconnected, reconnecting in %d seconds...", int(reconnectBackoff.Seconds()))
		select {
		case <-br.shortCircuitReconnectBackoff:
			br.Log.Debugln("Reconnect backoff was short-circuited")
		case <-time.After(reconnectBackoff):
		}
		if br.stopping {
			return
		}
	}
}
