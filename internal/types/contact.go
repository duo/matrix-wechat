package types

type ContactInfo struct {
	Uin    string
	Name   string
	Remark string
}

func NewContact(uin, name, remark string) *ContactInfo {
	return &ContactInfo{
		Uin:    uin,
		Name:   name,
		Remark: remark,
	}
}
