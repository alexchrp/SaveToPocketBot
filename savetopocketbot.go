package main

import (
	"encoding/json"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"log"
	"net/http"
	"os"
	"strconv"
)

var (
	URL             = "https://api.telegram.org/bot" + os.Getenv("TOKEN") + "/"
	ResponseHeaders = map[string]string{"Access-Control-Allow-Origin": "*", "Access-Control-Allow-Headers": "Origin, X-Requested-With, Content-Type, Accept"}
)

type ReceivedEvent struct {
	Body string `json:"body"`
}

type Body struct {
	Message Message `json:"message"`
}

type Message struct {
	User                 User   `json:"from"`
	ForwardFromUser      User   `json:"forward_from"`
	Chat                 Chat   `json:"chat"`
	ForwardFromChat      Chat   `json:"forward_from_chat"`
	ForwardFromMessageId int    `json:"forward_from_message_id"`
	Text                 string `json:"text"`
}

type User struct {
	Id        int    `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type Chat struct {
	Id       int    `json:"id"`
	Title    string `json:"title"`
	Type     string `json:"type"`
	Username string `json:"username"`
}

func sendMessage(text, chatId string) {
	finalText := "You said: " + text
	url := fmt.Sprintf("%ssendMessage?text=%s&chat_id=%s", URL, finalText, chatId)
	result, _ := http.Get(url)
	log.Println("Send message result: ", result)
}

func parseMessage(receivedEvent ReceivedEvent) Message {
	var event Body
	err := json.Unmarshal([]byte(receivedEvent.Body), &event)
	if err != nil {
		log.Printf("err was %v", err)
	}
	return event.Message
}

func HandleRequest(receivedEvent ReceivedEvent) (events.APIGatewayProxyResponse, error) {
	message := parseMessage(receivedEvent)
	chatId := message.Chat.Id
	sendMessage(message.Text, strconv.Itoa(chatId))
	return events.APIGatewayProxyResponse{StatusCode: 200, Headers: ResponseHeaders}, nil
}

func main() {
	lambda.Start(HandleRequest)
}
