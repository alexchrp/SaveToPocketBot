package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-lambda-go/lambda"
	"log"
	"net/http"
	"os"
	"strconv"
)

type Event struct {
	Message Message `json:"message"`
}

type Message struct {
	User                 User   `json:"user"`
	ForwardFromUser      User   `json:"forward_from"`
	Chat                 Chat   `json:"chat"`
	ForwardFromChat      Chat   `json:"forward_from_chat"`
	ForwardFromMessageId int    `json:"forward_from_message_id"`
	Text                 string `json:"text"`
}

type User struct {
	Id        int    `json:"id"`
	FirstName string `json:"first_name"`
}

type Chat struct {
	Id       int    `json:"id"`
	Title    string `json:"title"`
	Type     string `json:"type"`
	Username string `json:"username"`
}

var URL = "https://api.telegram.org/bot" + os.Getenv("TOKEN") + "/"

func sendMessage(text, chatId string) {
	finalText := "You said: " + text
	url := fmt.Sprintf("%ssendMessage?text=%s&chat_id=%s", URL, finalText, chatId)
	_, _ = http.Get(url)
}

func HandleRequest(ctx context.Context, event Event) (string, error) {
	message := event.Message
	bytes, _ := json.Marshal(event)
	log.Println("Message received = ", string(bytes))
	chatId := message.Chat.Id
	sendMessage(message.Text, strconv.Itoa(chatId))
	return "'statusCode': 200", nil
}

func main() {
	lambda.Start(HandleRequest)
}
