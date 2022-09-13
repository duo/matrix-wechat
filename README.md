# matrix-wechat
A Matrix-Wechat puppeting bridge based on [mautrix-go](https://github.com/mautrix/go).

### Documentation

All setup and usage instructions can refer to [docs.mau.fi]. Some quick links:

[docs.mau.fi]: https://docs.mau.fi/bridges/go/whatsapp/index.html

* [Bridge setup](https://docs.mau.fi/bridges/go/setup.html)
* [Agent setup](https://github.com/duo/matrix-wechat-agent)

### Features & roadmap

* Matrix → Wechat
  * [ ] Message types
    * [x] Text
	* [x] Image
	* [ ] Sticker
	* [x] Video
	* [ ] Audio
    * [x] File
    * [x] Mention
    * [ ] Reply
    * [ ] Location
  * [x] Chat types
	* [x] Direct
	* [x] Room
  * [ ] Presence
  * [ ] Redaction
  * [ ] Group actions
    * [ ] Join
    * [ ] Invite
    * [ ] Leave
    * [ ] Kick
	* [ ] Mute
  * [ ] Room metadata
    * [ ] Name
    * [ ] Avatar
    * [ ] Topic
  * [ ] User metadata
    * [ ] Name
    * [ ] Avatar

* QQ → Matrix
  * [ ] Message types
    * [x] Text
	* [x] Image
	* [ ] Sticker
	* [x] Video
	* [x] Audio
    * [x] File
    * [x] Mention
    * [x] Reply
    * [x] Location
  * [ ] Chat types
    * [x] Private
    * [x] Group
  * [ ] Presence
  * [ ] Redaction
  * [ ] Group actions
    * [ ] Invite
    * [ ] Join
    * [ ] Leave
    * [ ] Kick
	* [ ] Mute
  * [ ] Group metadata
    * [x] Name
    * [x] Avatar
	* [ ] Topic
  * [x] User metadata
    * [x] Name
    * [x] Avatar
  * [ ] Login types
	* [ ] Password
	* [ ] QR code

* Misc
  * [ ] Automatic portal creation
    * [ ] After login
    * [ ] When added to group
    * [x] When receiving message
  * [x] Double puppeting
