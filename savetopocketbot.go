package main

import (
	"encoding/json"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"log"
	"net/http"
	"os"
	"strconv"
)

var (
	tgUrl            = "https://api.telegram.org/bot" + os.Getenv("TOKEN") + "/"
	responseHeaders  = map[string]string{"Access-Control-Allow-Origin": "*", "Access-Control-Allow-Headers": "Origin, X-Requested-With, Content-Type, Accept"}
	ss, _            = session.NewSession(aws.NewConfig().WithRegion("eu-central-1"))
	db               = dynamodb.New(ss)
	pocketTokenTable = os.Getenv("POCKET_TOKEN_TABLE")
)

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
	url := fmt.Sprintf("%ssendMessage?text=%s&chat_id=%s", tgUrl, finalText, chatId)
	result, _ := http.Get(url)
	log.Println("Send message result: ", result)
}

func parseMessage(body string) Message {
	var event Body
	err := json.Unmarshal([]byte(body), &event)
	if err != nil {
		log.Printf("err was %v", err)
	}
	return event.Message
}

func HandleRequest(request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	message := parseMessage(request.Body)
	processMessage(message)
	return events.APIGatewayProxyResponse{StatusCode: 200, Headers: responseHeaders}, nil
}

func processMessage(message Message) {
	chatId := message.Chat.Id
	if message.Text == "/start" {
		createPocketApiToken(message.User)
	}
	sendMessage(message.Text, strconv.Itoa(chatId))
}

type UserToken struct {
	Id    string `json:"id"`
	Token string `json:"token"`
}

func createPocketApiToken(user User) {
	token, err := createToken(user)
	if err != nil {

	}
	item := UserToken{strconv.Itoa(user.Id), token}
	av, err := dynamodbattribute.MarshalMap(item)
	input := &dynamodb.PutItemInput{
		TableName: aws.String(pocketTokenTable),
		Item:      av,
	}
	_, err = db.PutItem(input)
	if err != nil {
		fmt.Println("Got error calling PutItem:")
		fmt.Println(err.Error())
	}
}

func createToken(user User) (string, error) {
	return "testToken", nil
}

func main() {
	lambda.Start(HandleRequest)
}
