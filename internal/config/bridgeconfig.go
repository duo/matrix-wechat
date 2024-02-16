package config

import (
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/duo/matrix-wechat/internal/types"

	"maunium.net/go/mautrix/bridge/bridgeconfig"
)

const (
	NameQualityName = 2
	NameQualityUin  = 1
)

type BridgeConfig struct {
	HomeserverProxy string `yaml:"hs_proxy"`

	UsernameTemplate    string `yaml:"username_template"`
	DisplaynameTemplate string `yaml:"displayname_template"`
	ListenAddress       string `yaml:"listen_address"`
	ListenSecret        string `yaml:"listen_secret"`

	PersonalFilteringSpaces  bool `yaml:"personal_filtering_spaces"`
	SpaceForOfficialAccounts bool `yaml:"space_for_official_accounts"`

	MessageStatusEvents bool `yaml:"message_status_events"`
	MessageErrorNotices bool `yaml:"message_error_notices"`
	PortalMessageBuffer int  `yaml:"portal_message_buffer"`

	AllowRedaction bool `yaml:"allow_redaction"`

	UserAvatarSync bool `yaml:"user_avatar_sync"`

	SyncDirectChatList    bool `yaml:"sync_direct_chat_list"`
	DefaultBridgePresence bool `yaml:"default_bridge_presence"`
	SendPresenceOnTyping  bool `yaml:"send_presence_on_typing"`

	DoublePuppetConfig bridgeconfig.DoublePuppetConfig `yaml:",inline"`

	PrivateChatPortalMeta string `yaml:"private_chat_portal_meta"`
	ParallelMemberSync    bool   `yaml:"parallel_member_sync"`
	ResendBridgeInfo      bool   `yaml:"resend_bridge_info"`
	MuteBridging          bool   `yaml:"mute_bridging"`
	AllowUserInvite       bool   `yaml:"allow_user_invite"`
	FederateRooms         bool   `yaml:"federate_rooms"`

	MessageHandlingTimeout struct {
		ErrorAfterStr string `yaml:"error_after"`
		DeadlineStr   string `yaml:"deadline"`

		ErrorAfter time.Duration `yaml:"-"`
		Deadline   time.Duration `yaml:"-"`
	} `yaml:"message_handling_timeout"`

	DisableBridgeAlerts bool `yaml:"disable_bridge_alerts"`

	CommandPrefix string `yaml:"command_prefix"`

	ManagementRoomText bridgeconfig.ManagementRoomTexts `yaml:"management_room_text"`

	Encryption bridgeconfig.EncryptionConfig `yaml:"encryption"`

	Permissions bridgeconfig.PermissionConfig `yaml:"permissions"`

	parsedUsernameTemplate *template.Template `yaml:"-"`
	displaynameTemplate    *template.Template `yaml:"-"`
}

type umBridgeConfig BridgeConfig

func (bc BridgeConfig) GetDoublePuppetConfig() bridgeconfig.DoublePuppetConfig {
	return bc.DoublePuppetConfig
}

func (bc BridgeConfig) GetEncryptionConfig() bridgeconfig.EncryptionConfig {
	return bc.Encryption
}

func (bc BridgeConfig) EnableMessageStatusEvents() bool {
	return bc.MessageStatusEvents
}

func (bc BridgeConfig) EnableMessageErrorNotices() bool {
	return bc.MessageErrorNotices
}

func (bc BridgeConfig) GetCommandPrefix() string {
	return bc.CommandPrefix
}

func (bc BridgeConfig) GetManagementRoomTexts() bridgeconfig.ManagementRoomTexts {
	return bc.ManagementRoomText
}

func (bc BridgeConfig) GetResendBridgeInfo() bool {
	return bc.ResendBridgeInfo
}

func boolToInt(val bool) int {
	if val {
		return 1
	}

	return 0
}

func (bc BridgeConfig) Validate() error {
	_, hasWildcard := bc.Permissions["*"]
	_, hasExampleDomain := bc.Permissions["example.com"]
	_, hasExampleUser := bc.Permissions["@admin:example.com"]
	exampleLen := boolToInt(hasWildcard) + boolToInt(hasExampleUser) + boolToInt(hasExampleDomain)
	if len(bc.Permissions) <= exampleLen {
		return errors.New("bridge.permissions not configured")
	}

	return nil
}

func (bc *BridgeConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	err := unmarshal((*umBridgeConfig)(bc))
	if err != nil {
		return err
	}

	bc.parsedUsernameTemplate, err = template.New("username").Parse(bc.UsernameTemplate)
	if err != nil {
		return err
	} else if !strings.Contains(bc.FormatUsername("1234567890"), "1234567890") {
		return fmt.Errorf("username template is missing user ID placeholder")
	}

	bc.displaynameTemplate, err = template.New("displayname").Parse(bc.DisplaynameTemplate)
	if err != nil {
		return err
	}

	if bc.MessageHandlingTimeout.ErrorAfterStr != "" {
		bc.MessageHandlingTimeout.ErrorAfter, err = time.ParseDuration(bc.MessageHandlingTimeout.ErrorAfterStr)
		if err != nil {
			return err
		}
	}
	if bc.MessageHandlingTimeout.DeadlineStr != "" {
		bc.MessageHandlingTimeout.Deadline, err = time.ParseDuration(bc.MessageHandlingTimeout.DeadlineStr)
		if err != nil {
			return err
		}
	}

	return nil
}

func (bc BridgeConfig) FormatDisplayname(contact types.ContactInfo) (string, int8) {
	var buf strings.Builder
	_ = bc.displaynameTemplate.Execute(&buf, contact)

	var quality int8
	switch {
	case len(contact.Name) > 0:
		quality = NameQualityName
	default:
		quality = NameQualityUin
	}

	return buf.String(), quality
}

func (bc BridgeConfig) FormatUsername(username string) string {
	var buf strings.Builder
	_ = bc.parsedUsernameTemplate.Execute(&buf, username)

	return buf.String()
}
