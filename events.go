package main

// Service messages ("Lan joined", "title changed", a call...): what happened, in words.

import (
	"fmt"
	"strings"

	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/tg"
)

func userName(e peer.Entities, id int64) string { return peerTitle(e, &tg.PeerUser{UserID: id}) }

func userNames(e peer.Entities, ids []int64) string {
	var n []string
	for _, id := range ids {
		n = append(n, userName(e, id))
	}
	return strings.Join(n, ", ")
}

func quoted(s string) string { return "“" + clean(s) + "”" }

func clock(sec int) string { return fmt.Sprintf("%d:%02d", sec/60, sec%60) }

func ttlText(sec int) string {
	switch {
	case sec <= 0:
		return "đã tắt tin tự xoá"
	case sec%604800 == 0:
		return fmt.Sprintf("đã đặt tin tự xoá sau %d tuần", sec/604800)
	case sec%86400 == 0:
		return fmt.Sprintf("đã đặt tin tự xoá sau %d ngày", sec/86400)
	case sec%3600 == 0:
		return fmt.Sprintf("đã đặt tin tự xoá sau %d giờ", sec/3600)
	}
	return fmt.Sprintf("đã đặt tin tự xoá sau %d giây", sec)
}

// eventText describes a service message action. The actor is already shown as the author of the line, so
// the text starts with the verb. from is the actor (to tell "left" from "removed").
func eventText(e peer.Entities, from tg.PeerClass, a tg.MessageActionClass) string {
	actor := int64(0)
	if u, ok := from.(*tg.PeerUser); ok {
		actor = u.UserID
	}
	switch v := a.(type) {
	case *tg.MessageActionChatCreate:
		return "đã tạo nhóm " + quoted(v.Title)
	case *tg.MessageActionChannelCreate:
		return "đã tạo " + quoted(v.Title)
	case *tg.MessageActionChatEditTitle:
		return "đã đổi tên thành " + quoted(v.Title)
	case *tg.MessageActionChatEditPhoto:
		return "đã đổi ảnh nhóm"
	case *tg.MessageActionChatDeletePhoto:
		return "đã xoá ảnh nhóm"
	case *tg.MessageActionChatAddUser:
		if len(v.Users) == 1 && v.Users[0] == actor {
			return "đã tham gia nhóm"
		}
		return "đã thêm " + userNames(e, v.Users) + " vào nhóm"
	case *tg.MessageActionChatDeleteUser:
		if v.UserID == actor {
			return "đã rời nhóm"
		}
		return "đã xoá " + userName(e, v.UserID) + " khỏi nhóm"
	case *tg.MessageActionChatJoinedByLink:
		return "đã tham gia bằng link mời"
	case *tg.MessageActionChatJoinedByRequest:
		return "đã tham gia theo yêu cầu"
	case *tg.MessageActionPinMessage:
		return "đã ghim một tin"
	case *tg.MessageActionHistoryClear:
		return "đã xoá lịch sử chat"
	case *tg.MessageActionScreenshotTaken:
		return "đã chụp màn hình"
	case *tg.MessageActionContactSignUp:
		return "đã tham gia Telegram"
	case *tg.MessageActionPhoneCall:
		kind := "cuộc gọi thoại"
		if v.Video {
			kind = "cuộc gọi video"
		}
		switch v.Reason.(type) {
		case *tg.PhoneCallDiscardReasonMissed:
			return kind + " nhỡ"
		case *tg.PhoneCallDiscardReasonBusy:
			return kind + " (máy bận)"
		}
		if d, ok := v.GetDuration(); ok && d > 0 {
			return kind + " · " + clock(d)
		}
		return kind
	case *tg.MessageActionGroupCall:
		if d, ok := v.GetDuration(); ok {
			return "cuộc gọi nhóm đã kết thúc · " + clock(d)
		}
		return "đã bắt đầu cuộc gọi nhóm"
	case *tg.MessageActionInviteToGroupCall:
		return "đã mời " + userNames(e, v.Users) + " vào cuộc gọi nhóm"
	case *tg.MessageActionTopicCreate:
		return "đã tạo topic " + quoted(v.Title)
	case *tg.MessageActionTopicEdit:
		if t, ok := v.GetTitle(); ok {
			return "đã đổi tên topic thành " + quoted(t)
		}
		if c, ok := v.GetClosed(); ok {
			if c {
				return "đã đóng topic"
			}
			return "đã mở lại topic"
		}
		return "đã sửa topic"
	case *tg.MessageActionSetMessagesTTL:
		return ttlText(v.Period)
	case *tg.MessageActionChatMigrateTo:
		return "nhóm đã nâng cấp thành siêu nhóm"
	case *tg.MessageActionChannelMigrateFrom:
		return "được nâng cấp từ nhóm " + quoted(v.Title)
	case *tg.MessageActionCustomAction:
		return clean(v.Message)
	case nil:
		return "sự kiện"
	}
	return "sự kiện: " + strings.TrimPrefix(fmt.Sprintf("%T", a), "*tg.MessageAction")
}
