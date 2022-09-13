package types

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"
)

const SEP_UID = "\u0001"

const (
	User  = "u"
	Group = "g"
)

// Some UIDs that are contacted often.
var (
	EmptyUID = UID{}
)

type UID struct {
	Uin  string
	Type string
}

func NewUserUID(uin string) UID {
	return UID{
		Uin:  uin,
		Type: User,
	}
}

func NewGroupUID(uin string) UID {
	return UID{
		Uin:  uin,
		Type: Group,
	}
}

func (u UID) IsUser() bool {
	return u.Type == User
}

func (u UID) IsGroup() bool {
	return u.Type == Group
}

func (u UID) String() string {
	return fmt.Sprintf("%s%s%s", u.Uin, SEP_UID, u.Type)
}

// MarshalText implements encoding.TextMarshaler for UID
func (u UID) MarshalText() ([]byte, error) {
	return []byte(u.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler for UID
func (u *UID) UnmarshalText(val []byte) error {
	out, err := ParseUID(string(val))
	if err != nil {
		return err
	}
	*u = out
	return nil
}

func (u UID) IsEmpty() bool {
	return len(u.Type) == 0
}

var _ sql.Scanner = (*UID)(nil)

// Scan scans the given SQL value into this UID.
func (u *UID) Scan(src interface{}) error {
	if src == nil {
		return nil
	}
	var out UID
	var err error
	switch val := src.(type) {
	case string:
		out, err = ParseUID(val)
	case []byte:
		out, err = ParseUID(string(val))
	default:
		err = fmt.Errorf("unsupported type %T for scanning UID", val)
	}
	if err != nil {
		return err
	}
	*u = out
	return nil
}

// Value returns the string representation of the UID as a value that the SQL package can use.
func (u UID) Value() (driver.Value, error) {
	if len(u.Type) == 0 {
		return nil, nil
	}
	return u.String(), nil
}

func ParseUID(uid string) (UID, error) {
	var parsed UID
	parts := strings.Split(uid, SEP_UID)
	if len(parts) != 2 {
		return parsed, fmt.Errorf("failed to parse UID: %s", uid)
	}
	return UID{
		Uin:  parts[0],
		Type: parts[1],
	}, nil
}
