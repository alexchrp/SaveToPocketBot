package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
)

var (
	tgUrl             = "https://api.telegram.org/bot" + os.Getenv("TG_TOKEN") + "/"
	responseHeaders   = map[string]string{"Access-Control-Allow-Origin": "*", "Access-Control-Allow-Headers": "Origin, X-Requested-With, Content-Type, Accept"}
	ss, _             = session.NewSession(aws.NewConfig().WithRegion("eu-central-1"))
	db                = dynamodb.New(ss)
	pocketTokenTable  = os.Getenv("POCKET_TOKEN_TABLE")
	pocketGetCodeUrl  = "https://getpocket.com/v3/oauth/request"
	pocketConsumerKey = os.Getenv("POCKET_CONSUMER_KEY")
	pocketRedirectUri = "https://telegram.me/SaveToPocketBot?start="
	pocketAuthUrl     = "https://getpocket.com/auth/authorize?request_token=%s&redirect_uri=" + pocketRedirectUri
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
	chatId := strconv.Itoa(message.Chat.Id)
	text := message.Text
	user := message.User
	var e error
	if text == "/start" {
		e = createPocketApiToken(user, chatId)
	} else if text == "/authorize" {
		e = createUserCode(user, chatId)
	} else {
		sendMessage(text, chatId)
	}
	if e != nil {
		log.Println("Error = ", e)
	}
}

func createUserCode(user User, chatId string) error {
	code, e := getUserCode()
	if e != nil {
		return e
	}
	log.Println("Code = ", code)
	e = saveCode(user, code)
	if e != nil {
		return e
	}
	sendAuthUrlAndInstruction(code, chatId)
	return nil
}

func saveCode(user User, code string) error {
	item := UserToken{strconv.Itoa(user.Id), "", code}
	av, err := dynamodbattribute.MarshalMap(item)
	input := &dynamodb.PutItemInput{
		TableName: aws.String(pocketTokenTable),
		Item:      av,
	}
	_, err = db.PutItem(input)
	if err != nil {
		fmt.Println("Got error calling PutItem:")
		fmt.Println(err.Error())
		return err
	}
	return nil
}

func sendAuthUrlAndInstruction(token, chatId string) {
	sendMessage(fmt.Sprintf(pocketAuthUrl, token)+"\n authorize you pocket via this url, "+
		"then you will be redirected back to bot and "+
		"need to press start, if redirection fails, then just send /start manually", chatId)
}

type UserToken struct {
	Id    string `json:"id"`
	Token string `json:"token"`
	Code  string `json:"code"`
}

func createPocketApiToken(user User, chatId string) error {
	var userToken UserToken
	if userToken, err := getUserToken(user); err != nil {
		log.Println("Error on getting user userToken:", err)
	} else if userToken == nil {
		e := createUserCode(user, chatId)
		return e
	} else if len(userToken.Token) > 0 {
		sendMessage("you have already been authorized", chatId)
	}
	code := userToken.Code
	t, err := createToken(code)
	if err != nil {
		fmt.Println("Got error on getting token from pocket:")
		fmt.Println(err.Error())
		return err
	}
	item := UserToken{strconv.Itoa(user.Id), t, code}
	av, err := dynamodbattribute.MarshalMap(item)
	input := &dynamodb.PutItemInput{
		TableName: aws.String(pocketTokenTable),
		Item:      av,
	}
	_, err = db.PutItem(input)
	if err != nil {
		fmt.Println("Got error calling PutItem:")
		fmt.Println(err.Error())
		return err
	}
	sendMessage("You have been authorized successfully", chatId)
	return nil
}

func getUserCode() (string, error) {
	authBody, _ := json.Marshal(map[string]string{
		"consumer_key": pocketConsumerKey,
		"redirect_uri": pocketRedirectUri,
	})

	req, err := http.NewRequest("POST", pocketGetCodeUrl, bytes.NewBuffer(authBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)

	var f interface{}
	err = json.Unmarshal(body, f)
	if err != nil {
		return "", err
	}
	m := f.(map[string]string)
	return m["code"], nil
}

func createToken(code string) (string, error) {
	authBody, _ := json.Marshal(map[string]string{
		"consumer_key": pocketConsumerKey,
		"code":         code,
	})

	req, err := http.NewRequest("POST", pocketGetCodeUrl, bytes.NewBuffer(authBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)

	var f interface{}
	err = json.Unmarshal(body, f)
	if err != nil {
		return "", err
	}
	m := f.(map[string]string)
	return m["access_token"], nil
}

func getUserToken(user User) (*UserToken, error) {
	input := &dynamodb.GetItemInput{
		TableName: aws.String(pocketTokenTable),
		Key: map[string]*dynamodb.AttributeValue{
			"id": {
				N: aws.String(strconv.Itoa(user.Id)),
			},
		},
	}
	tokenResult, err := db.GetItem(input)
	if err != nil {
		return nil, err
	}
	resultItem := tokenResult.Item
	if resultItem == nil {
		return nil, nil
	}
	item := UserToken{}
	err = dynamodbattribute.UnmarshalMap(resultItem, &item)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func main() {
	lambda.Start(HandleRequest)
}
