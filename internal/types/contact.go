package types

type ContactInfo struct {
	Uin  string
	Name string
}

func NewContact(uin string, name string) *ContactInfo {
	return &ContactInfo{
		Uin:  uin,
		Name: name,
	}
}
