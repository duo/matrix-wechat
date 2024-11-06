package config

import (
	"maunium.net/go/mautrix/bridge/bridgeconfig"

	up "go.mau.fi/util/configupgrade"
)

func DoUpgrade(helper *up.Helper) {
	bridgeconfig.Upgrader.DoUpgrade(helper)

	helper.Copy(up.Str, "bridge", "hs_proxy")
	helper.Copy(up.Str, "bridge", "username_template")
	helper.Copy(up.Str, "bridge", "displayname_template")
	helper.Copy(up.Str, "bridge", "listen_address")
	helper.Copy(up.Str, "bridge", "listen_secret")
	helper.Copy(up.Bool, "bridge", "personal_filtering_spaces")
	helper.Copy(up.Bool, "bridge", "message_status_events")
	helper.Copy(up.Bool, "bridge", "message_error_notices")
	helper.Copy(up.Int, "bridge", "portal_message_buffer")
	helper.Copy(up.Bool, "bridge", "allow_redaction")
	helper.Copy(up.Bool, "bridge", "user_avatar_sync")
	helper.Copy(up.Bool, "bridge", "sync_direct_chat_list")
	helper.Copy(up.Bool, "bridge", "default_bridge_presence")
	helper.Copy(up.Bool, "bridge", "send_presence_on_typing")
	helper.Copy(up.Map, "bridge", "double_puppet_server_map")
	helper.Copy(up.Bool, "bridge", "double_puppet_allow_discovery")
	if legacySecret, ok := helper.Get(up.Str, "bridge", "login_shared_secret"); ok && len(legacySecret) > 0 {
		baseNode := helper.GetBaseNode("bridge", "login_shared_secret_map")
		baseNode.Map[helper.GetBase("homeserver", "domain")] = up.StringNode(legacySecret)
		baseNode.UpdateContent()
	} else {
		helper.Copy(up.Map, "bridge", "login_shared_secret_map")
	}
	if legacyPrivateChatPortalMeta, ok := helper.Get(up.Bool, "bridge", "private_chat_portal_meta"); ok {
		updatedPrivateChatPortalMeta := "default"
		if legacyPrivateChatPortalMeta == "true" {
			updatedPrivateChatPortalMeta = "always"
		}
		helper.Set(up.Str, updatedPrivateChatPortalMeta, "bridge", "private_chat_portal_meta")
	} else {
		helper.Copy(up.Str, "bridge", "private_chat_portal_meta")
	}
	helper.Copy(up.Bool, "bridge", "parallel_member_sync")
	helper.Copy(up.Bool, "bridge", "resend_bridge_info")
	helper.Copy(up.Bool, "bridge", "mute_bridging")
	helper.Copy(up.Bool, "bridge", "allow_user_invite")
	helper.Copy(up.Str, "bridge", "command_prefix")
	helper.Copy(up.Bool, "bridge", "federate_rooms")
	helper.Copy(up.Bool, "bridge", "disable_bridge_alerts")
	helper.Copy(up.Str|up.Null, "bridge", "message_handling_timeout", "error_after")
	helper.Copy(up.Str|up.Null, "bridge", "message_handling_timeout", "deadline")

	helper.Copy(up.Str, "bridge", "management_room_text", "welcome")
	helper.Copy(up.Str, "bridge", "management_room_text", "welcome_connected")
	helper.Copy(up.Str, "bridge", "management_room_text", "welcome_unconnected")
	helper.Copy(up.Str|up.Null, "bridge", "management_room_text", "additional_help")
	helper.Copy(up.Bool, "bridge", "encryption", "allow")
	helper.Copy(up.Bool, "bridge", "encryption", "default")
	helper.Copy(up.Bool, "bridge", "encryption", "require")
	helper.Copy(up.Bool, "bridge", "encryption", "appservice")
	helper.Copy(up.Bool, "bridge", "encryption", "plaintext_mentions")
	helper.Copy(up.Bool, "bridge", "encryption", "delete_keys", "delete_outbound_on_ack")
	helper.Copy(up.Bool, "bridge", "encryption", "delete_keys", "dont_store_outbound")
	helper.Copy(up.Bool, "bridge", "encryption", "delete_keys", "ratchet_on_decrypt")
	helper.Copy(up.Bool, "bridge", "encryption", "delete_keys", "delete_fully_used_on_decrypt")
	helper.Copy(up.Bool, "bridge", "encryption", "delete_keys", "delete_prev_on_new_session")
	helper.Copy(up.Bool, "bridge", "encryption", "delete_keys", "delete_on_device_delete")
	helper.Copy(up.Bool, "bridge", "encryption", "delete_keys", "periodically_delete_expired")
	helper.Copy(up.Bool, "bridge", "encryption", "delete_keys", "delete_outdated_inbound")
	helper.Copy(up.Str, "bridge", "encryption", "verification_levels", "receive")
	helper.Copy(up.Str, "bridge", "encryption", "verification_levels", "send")
	helper.Copy(up.Str, "bridge", "encryption", "verification_levels", "share")

	legacyKeyShareAllow, ok := helper.Get(up.Bool, "bridge", "encryption", "key_sharing", "allow")
	if ok {
		helper.Set(up.Bool, legacyKeyShareAllow, "bridge", "encryption", "allow_key_sharing")
		legacyKeyShareRequireCS, legacyOK1 := helper.Get(up.Bool, "bridge", "encryption", "key_sharing", "require_cross_signing")
		legacyKeyShareRequireVerification, legacyOK2 := helper.Get(up.Bool, "bridge", "encryption", "key_sharing", "require_verification")
		if legacyOK1 && legacyOK2 && legacyKeyShareRequireVerification == "false" && legacyKeyShareRequireCS == "false" {
			helper.Set(up.Str, "unverified", "bridge", "encryption", "verification_levels", "share")
		}
	} else {
		helper.Copy(up.Bool, "bridge", "encryption", "allow_key_sharing")
	}

	helper.Copy(up.Bool, "bridge", "encryption", "rotation", "enable_custom")
	helper.Copy(up.Int, "bridge", "encryption", "rotation", "milliseconds")
	helper.Copy(up.Int, "bridge", "encryption", "rotation", "messages")
	helper.Copy(up.Bool, "bridge", "encryption", "rotation", "disable_device_change_key_rotation")
	helper.Copy(up.Map, "bridge", "permissions")
}

var SpacedBlocks = [][]string{
	{"homeserver", "software"},
	{"appservice"},
	{"appservice", "hostname"},
	{"appservice", "database"},
	{"appservice", "id"},
	{"appservice", "as_token"},
	{"bridge"},
	{"bridge", "command_prefix"},
	{"bridge", "management_room_text"},
	{"bridge", "encryption"},
	{"bridge", "permissions"},
	{"logging"},
}
