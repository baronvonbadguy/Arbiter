package main

import (
	"log"
	"strings"
	"fmt"
	tb "gopkg.in/tucnak/telebot.v2"
	"time"
	"github.com/go-redis/redis"
)

func getUsersActivePath(userID int) *Path{
	key := fmt.Sprintf("user:%d:activePath", userID)
	pathData, err := R.Get(key).Result()
	if err != nil {
		return nil
	} else {
		// decode path data into native struct
		p := Decode([]byte(pathData))
		return &p
	}
}

func begin(m *tb.Message, p Path) {
	// clear out any existing active path
	key := fmt.Sprintf("user:%d:activePath", m.Sender.ID)
	err := R.Del(key).Err()
	if err != nil {
		log.Printf("unable to delete active path for %d %s", m.Sender.ID, err)
		return
	}
	// remove button name from path messasge building
	m.Text = ""
	// take the first step
	step(m, &p)
}

func step(m *tb.Message, p *Path) error {
	key := fmt.Sprintf("user:%d:activePath", m.Sender.ID)
	// if the incoming message has text append it to the list of responses.
	if m.Text != "" {
		p.Responses = append(p.Responses, m.Text)
	}
	// if all of the prompts have been sent to the user call the function
	// at the end of the path, and pass in the responses joined by a semicolon
	if p.Index == len(p.Prompts) {
		m.Text = strings.Join(p.Responses, ";")
		log.Printf("reached the end of the path, calling %s with text %s", p.Command, m.Text)
		// grab function from map of telegram command names to their respsective
		// native go functions
		if f, ok := Functions[p.Command]; ok {
			f(m)
		} else {
			log.Printf("function %s cannot be found", p.Command)
		}
		// delete the path state since it has been fully traversed
		_, err := R.Del(key).Result()
		if err != nil {
			log.Printf("unable to delete active path for user %d %s", m.Sender.ID, err)
		}
		return nil
	}
	// if there is still a prompt in the list, send it, increment the index
	// and update the saved state data with a reset TTL
	B.Send(m.Sender, fmt.Sprint(p.Prompts[p.Index]))
	p.Index += 1
	R.Set(key, Encode(*p), time.Minute)
	return nil
}

func registerStaticCommand(userID int, name string, text string) (error) {
	chat, _, _ := getUsersActiveChat(userID)
	key := fmt.Sprintf("chat:%d:commands", chat)
	err := R.HSet(key, name, text).Err()
	return err
}

func unregisterStaticCommand(userID int, name string) (error) {
	chanID, _, _ := getUsersActiveChat(userID)
	key := fmt.Sprintf("chat:%s:commands", chanID)
	err := R.HDel(key, name).Err()
	return err
}

func getUsersActiveChat(userID int) (int, string, error) {
	key := fmt.Sprintf("user:%d:activeChat", userID)
	activeChatID, err := R.Get(key).Int64()
	if err != nil {
		log.Printf("userID %d doesn't have an active chat but tried to access it", userID)
		return 0, "", err
	}
	key = fmt.Sprintf("chat:%d:title", activeChatID)
	var chatName string
	if chatName, err = R.Get(key).Result(); err != nil {
		log.Printf("could not access title for chat %s", userID)
		return 0, "", err
	}
	return int(activeChatID), chatName, nil
}

func getReplyKeyboardForCommands(commands []FunctionButton) ([][]tb.ReplyButton) {
	keys := [][]tb.ReplyButton{}
	row := []tb.ReplyButton{}
	for _, v := range commands {
		button := tb.ReplyButton{Text: v.Label}
		B.Handle(&button, v.Function)
		row = append(row, button)
	}
	keys = append(keys, row)
	return keys
}

func getInlineKeyboardForCommands(commands []FunctionButton) ([][]tb.InlineButton) {
	keys := [][]tb.InlineButton{}
	row := []tb.InlineButton{}
	for _, v := range commands {
		button := tb.InlineButton{Text: v.Label}
		B.Handle(&button, v)
		row = append(row, button)
	}
	keys = append(keys, row)
	return keys
}

func getInlineButtonForMessages(buttonTexts []string) ([][]tb.InlineButton) {
	keys := [][]tb.InlineButton{}
	row := []tb.InlineButton{}
	for _, t := range buttonTexts {
		button := tb.InlineButton{
			Unique: "",
			Text:   t}
		B.Handle(&button, t)
		row = append(row, button)
	}
	keys = append(keys, row)
	return keys
}

func setUsersActiveChat(userID int, chatID int64) {
	key := fmt.Sprintf("user:%d:activeChat", userID)
	if err := R.Set(key, chatID, 0).Err(); err != redis.Nil {
		log.Printf("%s set to %d", key, userID)
	} else {
		log.Printf("failed to set %s to %d ", key, userID)
	}

}
func switchChat(m *tb.Message) {
	split := strings.Split(strings.Replace(m.Text, "/switchChat ", "", 1), " ")
	chatName := split[0]
	hKey := fmt.Sprintf("user:%d:activeChat", m.Sender.ID)
	R.Set(hKey, chatName, 0)
	B.Send(m.Sender, fmt.Sprintf("switched to managing chat %s", chatName))
}

func addAdmin(m *tb.Message) {
	split := strings.Split(strings.Replace(m.Text, "/addAdmin ", "", 1), " ")
	adminName := split[0]
	chanID, _, _ := getUsersActiveChat(m.Sender.ID)
	setKey := fmt.Sprintf("chat:%d:admins", chanID)
	R.SAdd(setKey, adminName)
	val, _ := R.SMembers(setKey).Result()
	B.Send(m.Sender, fmt.Sprintf("admins now %s", val))
}

func removeAdmin(m *tb.Message) {
	split := strings.Split(strings.Replace(m.Text, "/removeAdmin ", "", 1), " ")
	adminName := split[0]
	chanID, chanTitle, _ := getUsersActiveChat(m.Sender.ID)
	setKey := fmt.Sprintf("chat:%d:admins", chanID)
	R.SRem(setKey, adminName)
	val, _ := R.SMembers(setKey).Result()
	B.Send(m.Sender, fmt.Sprintf("admins for chat %s now %s", chanTitle, val))
}

func listAdmins(m *tb.Message) {
	chanID, chanTitle, _ := getUsersActiveChat(m.Sender.ID)
	setKey := fmt.Sprintf("chat:%d:admins", chanID)
	val, _ := R.SMembers(setKey).Result()
	B.Send(m.Sender, fmt.Sprintf("admins for chat %s now %s", chanTitle, val))
}

func addCommand(m *tb.Message) {
	log.Printf("entered addCommand with msg %s", m.Text)
	split := strings.Split(strings.Replace(m.Text, "/addCommand ", "", 1), ";")
	commandName := split[0]
	if ! strings.HasPrefix(commandName, "/") {
		commandName = "/" + commandName
	}
	commandText := split[1]
	if len(split) == 1 {
		B.Send(m.Sender, fmt.Sprint(
			"you need to specify a command and response to add, such as /addCommand commandName;response text"))
	}
	if err := registerStaticCommand(m.Sender.ID, commandName, commandText); err != nil {
		msg := fmt.Sprintf("error while trying to add command %s", commandName)
		B.Send(m.Sender, msg)
		log.Printf(fmt.Sprintf("%s: %s"), msg, err)
	}
	B.Send(m.Sender, fmt.Sprintf("added/updated command %s", commandName))
}

func removeCommand(m *tb.Message) {
	log.Printf("entered removeCommand with msg %s", m.Text)
	commandName := strings.Replace(m.Text, "/removeCommand ", "", 1)
	if ! strings.HasPrefix(commandName, "/") {
		commandName = "/" + commandName
	}
	if err := unregisterStaticCommand(m.Sender.ID, commandName); err != nil {
		msg := fmt.Sprintf("error while trying to remove command %s", commandName)
		B.Send(m.Sender, msg)
		log.Printf(fmt.Sprintf("%s: %s"), msg, err)
	}
	B.Send(m.Sender, fmt.Sprintf("removed command %s", commandName))
}

func listCommands(m *tb.Message) {
	chanID, chanTitle, _ := getUsersActiveChat(m.Sender.ID)
	key := fmt.Sprintf("chanID:%d:commands", chanID)
	val, _ := R.HKeys(key).Result()
	B.Send(m.Sender, fmt.Sprintf("commands for chanID %s %s", chanTitle, val))
}

func addChat(m *tb.Message) {
	link := fmt.Sprintf("https://telegram.me/beru_dev_bot?startgroup=%d", m.Sender.ID)
	keys := [][]tb.InlineButton{}
	row := []tb.InlineButton{}
	button := tb.InlineButton{
		Unique: "invite_link",
		Text:   "Invite Link",
		URL:    link,
	}
	row = append(row, button)
	keys = append(keys, row)
	B.Send(m.Sender, "Click this button to invite beru to your chat", &tb.ReplyMarkup{
		InlineKeyboard: keys,
	})
}
