package internal

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/duo/matrix-wechat/internal/types"
	"github.com/duo/matrix-wechat/internal/wechat"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridge/commands"
	"maunium.net/go/mautrix/bridge/status"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

const loginTimeout = 2 * time.Minute

type WrappedCommandEvent struct {
	*commands.Event
	Bridge *WechatBridge
	User   *User
	Portal *Portal
}

func (br *WechatBridge) RegisterCommands() {
	proc := br.CommandProcessor.(*commands.Processor)
	proc.AddHandlers(
		cmdLogin,
		cmdLogout,
		cmdPing,
		cmdDeletePortal,
		cmdDeleteAllPortals,
		cmdList,
		cmdSearch,
		cmdSync,
	)
}

func wrapCommand(handler func(*WrappedCommandEvent)) func(*commands.Event) {
	return func(ce *commands.Event) {
		user := ce.User.(*User)
		var portal *Portal
		if ce.Portal != nil {
			portal = ce.Portal.(*Portal)
		}
		br := ce.Bridge.Child.(*WechatBridge)
		handler(&WrappedCommandEvent{ce, br, user, portal})
	}
}

var (
	HelpSectionConnectionManagement = commands.HelpSection{Name: "Connection management", Order: 11}
	HelpSectionCreatingPortals      = commands.HelpSection{Name: "Creating portals", Order: 15}
	HelpSectionPortalManagement     = commands.HelpSection{Name: "Portal management", Order: 20}
	HelpSectionInvites              = commands.HelpSection{Name: "Group invites", Order: 25}
	HelpSectionMiscellaneous        = commands.HelpSection{Name: "Miscellaneous", Order: 30}
)

var cmdLogin = &commands.FullHandler{
	Func: wrapCommand(fnLogin),
	Name: "login",
	Help: commands.HelpMeta{
		Section:     commands.HelpSectionAuth,
		Description: "Link the bridge to your WeChat account.",
	},
}

func fnLogin(ce *WrappedCommandEvent) {
	if ce.User.IsLoggedIn() {
		ce.Reply("You're already logged in")
		return
	}

	err := ce.User.Connect()
	if err != nil {
		ce.User.log.Errorf("Failed to connect:", err)
		ce.Reply("Failed to connect: %v", err)
		return
	}

	qrCode := ce.User.LoginWtihQRCode()
	if len(qrCode) == 0 {
		ce.Reply("Get QR code timed out. Please restart the login.")
		return
	}

	qrEventID, err := ce.User.sendQR(ce, qrCode)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	defer cancel()

	for {
		if ce.User.IsLoggedIn() {
			ce.User.MarkLogin()

			_, _ = ce.Bot.RedactEvent(ce.RoomID, qrEventID)
			ce.Reply("Login successful.")
			break
		}

		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			ce.Reply("Timedout, Please restart the login.")
			return
		}
	}
}

func (u *User) sendQR(ce *WrappedCommandEvent, qrCode []byte) (id.EventID, error) {
	url, err := u.uploadQR(ce, qrCode)
	if err != nil {
		return "", err
	}
	content := event.MessageEventContent{
		MsgType: event.MsgImage,
		Body:    "",
		URL:     url.CUString(),
	}
	resp, err := ce.Bot.SendMessageEvent(ce.RoomID, event.EventMessage, &content)
	if err != nil {
		u.log.Errorln("Failed to send edited QR code to user:", err)
	}
	return resp.EventID, nil
}

func (u *User) uploadQR(ce *WrappedCommandEvent, qrCode []byte) (id.ContentURI, error) {
	bot := u.bridge.AS.BotClient()

	resp, err := bot.UploadBytes(qrCode, "image/png")
	if err != nil {
		u.log.Errorln("Failed to upload QR code:", err)
		ce.Reply("Failed to upload QR code: %v", err)
		return id.ContentURI{}, err
	}
	return resp.ContentURI, nil
}

var cmdLogout = &commands.FullHandler{
	Func: wrapCommand(fnLogout),
	Name: "logout",
	Help: commands.HelpMeta{
		Section:     commands.HelpSectionAuth,
		Description: "Unlink the bridge from your WeChat account.",
	},
}

func fnLogout(ce *WrappedCommandEvent) {
	if !ce.User.IsLoggedIn() {
		ce.Reply("You are not connected to WeChat.")
		return
	}
	puppet := ce.Bridge.GetPuppetByUID(ce.User.UID)
	if puppet.CustomMXID != "" {
		err := puppet.SwitchCustomMXID("", "")
		if err != nil {
			ce.User.log.Warnln("Failed to logout-matrix while logging out of WeChat:", err)
		}
	}
	ce.User.removeFromUIDMap(status.BridgeState{StateEvent: status.StateLoggedOut})
	ce.User.DeleteConnection()
	ce.User.DeleteSession()
	ce.Reply("Logged out successfully.")
}

var cmdPing = &commands.FullHandler{
	Func: wrapCommand(fnPing),
	Name: "ping",
	Help: commands.HelpMeta{
		Section:     HelpSectionConnectionManagement,
		Description: "Check your connection to WeChat.",
	},
}

func fnPing(ce *WrappedCommandEvent) {
	if ce.User.IsLoggedIn() {
		ce.Reply("Logged in as %s, connection to WeChat OK (probably)", ce.User.UID.Uin)
	} else {
		ce.Reply("You're not logged into WeChat.")
	}
}

func canDeletePortal(portal *Portal, userID id.UserID) bool {
	if len(portal.MXID) == 0 {
		return false
	}

	members, err := portal.MainIntent().JoinedMembers(portal.MXID)
	if err != nil {
		portal.log.Errorfln("Failed to get joined members to check if portal can be deleted by %s: %v", userID, err)
		return false
	}
	for otherUser := range members.Joined {
		_, isPuppet := portal.bridge.ParsePuppetMXID(otherUser)
		if isPuppet || otherUser == portal.bridge.Bot.UserID || otherUser == userID {
			continue
		}
		user := portal.bridge.GetUserByMXID(otherUser)
		if user != nil {
			return false
		}
	}
	return true
}

var cmdDeletePortal = &commands.FullHandler{
	Func: wrapCommand(fnDeletePortal),
	Name: "delete-portal",
	Help: commands.HelpMeta{
		Section:     HelpSectionPortalManagement,
		Description: "Delete the current portal. If the portal is used by other people, this is limited to bridge admins.",
	},
	RequiresPortal: true,
}

func fnDeletePortal(ce *WrappedCommandEvent) {
	if !ce.User.Admin && !canDeletePortal(ce.Portal, ce.User.MXID) {
		ce.Reply("Only bridge admins can delete portals with other Matrix users")
		return
	}

	ce.Portal.log.Infoln(ce.User.MXID, "requested deletion of portal.")
	ce.Portal.Delete()
	ce.Portal.Cleanup(false)
}

var cmdDeleteAllPortals = &commands.FullHandler{
	Func: wrapCommand(fnDeleteAllPortals),
	Name: "delete-all-portals",
	Help: commands.HelpMeta{
		Section:     HelpSectionPortalManagement,
		Description: "Delete all portals.",
	},
}

func fnDeleteAllPortals(ce *WrappedCommandEvent) {
	portals := ce.Bridge.GetAllPortals()
	var portalsToDelete []*Portal

	if ce.User.Admin {
		portalsToDelete = portals
	} else {
		portalsToDelete = portals[:0]
		for _, portal := range portals {
			if canDeletePortal(portal, ce.User.MXID) {
				portalsToDelete = append(portalsToDelete, portal)
			}
		}
	}
	if len(portalsToDelete) == 0 {
		ce.Reply("Didn't find any portals to delete")
		return
	}

	leave := func(portal *Portal) {
		if len(portal.MXID) > 0 {
			_, _ = portal.MainIntent().KickUser(portal.MXID, &mautrix.ReqKickUser{
				Reason: "Deleting portal",
				UserID: ce.User.MXID,
			})
		}
	}
	customPuppet := ce.Bridge.GetPuppetByCustomMXID(ce.User.MXID)
	if customPuppet != nil && customPuppet.CustomIntent() != nil {
		intent := customPuppet.CustomIntent()
		leave = func(portal *Portal) {
			if len(portal.MXID) > 0 {
				_, _ = intent.LeaveRoom(portal.MXID)
				_, _ = intent.ForgetRoom(portal.MXID)
			}
		}
	}
	ce.Reply("Found %d portals, deleting...", len(portalsToDelete))
	for _, portal := range portalsToDelete {
		portal.Delete()
		leave(portal)
	}
	ce.Reply("Finished deleting portal info. Now cleaning up rooms in background.")

	go func() {
		for _, portal := range portalsToDelete {
			portal.Cleanup(false)
		}
		ce.Reply("Finished background cleanup of deleted portal rooms.")
	}()
}

func matchesQuery(str string, query string) bool {
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(str), query)
}

func formatContacts(bridge *WechatBridge, input []*wechat.UserInfo, query string) (result []string) {
	hasQuery := len(query) > 0
	for _, contact := range input {
		if len(contact.Name) == 0 {
			continue
		}
		uid := types.NewUserUID(contact.ID)
		puppet := bridge.GetPuppetByUID(uid)

		if !hasQuery || matchesQuery(contact.Name, query) || matchesQuery(contact.Remark, query) || matchesQuery(uid.Uin, query) {
			result = append(result, fmt.Sprintf("* %s / [%s](https://matrix.to/#/%s) - `%s`", contact.Name, contact.Remark, puppet.MXID, uid.Uin))
		}
	}
	sort.Strings(result)
	return
}

func formatGroups(input []*wechat.GroupInfo, query string) (result []string) {
	hasQuery := len(query) > 0
	for _, group := range input {
		if !hasQuery || matchesQuery(group.Name, query) || matchesQuery(group.ID, query) {
			result = append(result, fmt.Sprintf("* %s - `%s`", group.Name, group.ID))
		}
	}
	sort.Strings(result)
	return
}

var cmdList = &commands.FullHandler{
	Func: wrapCommand(fnList),
	Name: "list",
	Help: commands.HelpMeta{
		Section:     HelpSectionMiscellaneous,
		Description: "Get a list of all contacts and groups.",
		Args:        "<`contacts`|`groups`> [_page_] [_items per page_]",
	},
	RequiresLogin: true,
}

func fnList(ce *WrappedCommandEvent) {
	if len(ce.Args) == 0 {
		ce.Reply("**Usage:** `list <contacts|groups> [page] [items per page]`")
		return
	}
	mode := strings.ToLower(ce.Args[0])
	if mode[0] != 'g' && mode[0] != 'c' {
		ce.Reply("**Usage:** `list <contacts|groups> [page] [items per page]`")
		return
	}
	var err error
	page := 1
	max := 100
	if len(ce.Args) > 1 {
		page, err = strconv.Atoi(ce.Args[1])
		if err != nil || page <= 0 {
			ce.Reply("\"%s\" isn't a valid page number", ce.Args[1])
			return
		}
	}
	if len(ce.Args) > 2 {
		max, err = strconv.Atoi(ce.Args[2])
		if err != nil || max <= 0 {
			ce.Reply("\"%s\" isn't a valid number of items per page", ce.Args[2])
			return
		} else if max > 400 {
			ce.Reply("Warning: a high number of items per page may fail to send a reply")
		}
	}

	contacts := mode[0] == 'c'
	typeName := "Groups"
	var result []string
	if contacts {
		typeName = "Contacts"
		result = formatContacts(ce.User.bridge, ce.User.Client.GetFriendList(), "")
	} else {
		result = formatGroups(ce.User.Client.GetGroupList(), "")
	}

	if len(result) == 0 {
		ce.Reply("No %s found", strings.ToLower(typeName))
		return
	}
	pages := int(math.Ceil(float64(len(result)) / float64(max)))
	if (page-1)*max >= len(result) {
		if pages == 1 {
			ce.Reply("There is only 1 page of %s", strings.ToLower(typeName))
		} else {
			ce.Reply("There are %d pages of %s", pages, strings.ToLower(typeName))
		}
		return
	}
	lastIndex := page * max
	if lastIndex > len(result) {
		lastIndex = len(result)
	}
	result = result[(page-1)*max : lastIndex]
	ce.Reply("### %s (page %d of %d)\n\n%s", typeName, page, pages, strings.Join(result, "\n"))
}

var cmdSearch = &commands.FullHandler{
	Func: wrapCommand(fnSearch),
	Name: "search",
	Help: commands.HelpMeta{
		Section:     HelpSectionMiscellaneous,
		Description: "Search for contacts or groups.",
		Args:        "<_query_>",
	},
	RequiresLogin: true,
}

func fnSearch(ce *WrappedCommandEvent) {
	if len(ce.Args) == 0 {
		ce.Reply("**Usage:** `search <query>`")
		return
	}

	query := strings.ToLower(strings.TrimSpace(strings.Join(ce.Args, " ")))
	formattedContacts := strings.Join(formatContacts(ce.User.bridge, ce.User.Client.GetFriendList(), query), "\n")
	formattedGroups := strings.Join(formatGroups(ce.User.Client.GetGroupList(), query), "\n")

	result := make([]string, 0, 2)
	if len(formattedContacts) > 0 {
		result = append(result, "### Contacts\n\n"+formattedContacts)
	}
	if len(formattedGroups) > 0 {
		result = append(result, "### Groups\n\n"+formattedGroups)
	}

	if len(result) == 0 {
		ce.Reply("No contacts or groups found")
		return
	}

	ce.Reply(strings.Join(result, "\n\n"))
}

var cmdSync = &commands.FullHandler{
	Func: wrapCommand(fnSync),
	Name: "sync",
	Help: commands.HelpMeta{
		Section:     HelpSectionMiscellaneous,
		Description: "Synchronize data from WeChat.",
		Args:        "<contacts/groups/space> [--contact-avatars] [--create-portals]",
	},
	RequiresLogin: true,
}

func fnSync(ce *WrappedCommandEvent) {
	args := strings.ToLower(strings.Join(ce.Args, " "))
	contacts := strings.Contains(args, "contacts")
	space := strings.Contains(args, "space")
	groups := strings.Contains(args, "groups") || space
	if !contacts && !space && !groups {
		ce.Reply("**Usage:** `sync <contacts/groups/space> [--contact-avatars] [--create-portals]`")
		return
	}
	createPortals := strings.Contains(args, "--create-portals")
	contactAvatars := strings.Contains(args, "--contact-avatars")
	if contactAvatars && !contacts {
		ce.Reply("`--contact-avatars` can only be used with `sync contacts`")
		return
	}

	if contacts {
		err := ce.User.ResyncContacts(contactAvatars)
		if err != nil {
			ce.Reply("Error resyncing contacts: %v", err)
		} else {
			ce.Reply("Resynced contacts")
		}
	}
	if space {
		if !ce.Bridge.Config.Bridge.PersonalFilteringSpaces {
			ce.Reply("Personal filtering spaces are not enabled on this instance of the bridge")
			return
		}
		if !ce.Bridge.Config.Bridge.SpaceForOfficialAccounts {
			count := 0
			chatsToAdd := ce.Bridge.DB.Portal.FindAllChatsNotInSpace(ce.User.UID)
			for _, key := range chatsToAdd {
				portal := ce.Bridge.GetPortalByUID(key)
				portal.addToSpace(ce.User)
				count++
			}
			plural := "s"
			if count == 1 {
				plural = ""
			}
			println("[DEBUG] Added", plural, "room"+plural+" to space")
			ce.Reply("Added %d room%s to space", count, plural)
		} else {
			privateChatsToAdd := ce.Bridge.DB.Portal.FindPrivateChatsNotInSpace(ce.User.UID)
			officialAccountsToAdd := ce.Bridge.DB.Portal.FindOfficialAccountsNotInOASpace(ce.User.UID)
			officialAccountsToRemove := ce.Bridge.DB.Portal.FindOfficialAccountsInDefaultSpace(ce.User.UID)
			privateAdded := 0
			officialAdded := 0
			officialRemoved := 0
			for _, key := range privateChatsToAdd {
				portal := ce.Bridge.GetPortalByUID(key)
				portal.addToSpace(ce.User)
				privateAdded++
			}
			for _, key := range officialAccountsToAdd {
				portal := ce.Bridge.GetPortalByUID(key)
				portal.addToOfficialAccountSpace(ce.User)
				officialAdded++
			}
			for _, key := range officialAccountsToRemove {
				portal := ce.Bridge.GetPortalByUID(key)
				portal.removeFromSpace(ce.User)
				officialRemoved++
			}
			privateAddedPlural := "s"
			officialAddedPlural := "s"
			if privateAdded == 1 {
				privateAddedPlural = ""
			}
			if officialAdded == 1 {
				officialAddedPlural = ""
			}
			officialRemovedMessage := ""
			if officialRemoved > 0 {
				plural := "s"
				if officialRemoved == 1 {
					plural = ""
				}
				officialRemovedMessage = ", and removed " + strconv.Itoa(officialRemoved) + " official account" + plural + " from space"
			}
			println("[DEBUG] Added", privateAdded, "DM room"+privateAddedPlural+" to space"+officialRemovedMessage)
			ce.Reply("Added %d DM room%s and %d official account%s to space"+officialRemovedMessage,
				privateAdded, privateAddedPlural, officialAdded, officialAddedPlural)
		}
	}
	if groups {
		err := ce.User.ResyncGroups(createPortals)
		if err != nil {
			ce.Reply("Error resyncing groups: %v", err)
		} else {
			ce.Reply("Resynced groups")
		}
	}
}
