package main

import (
	"bytes"
	"encoding/json"
	"errors"
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
	"net/url"
	"os"
	"strconv"
)

var (
	tgUrl             = "https://api.telegram.org/"
	responseHeaders   = map[string]string{"Access-Control-Allow-Origin": "*", "Access-Control-Allow-Headers": "Origin, X-Requested-With, Content-Type, Accept"}
	ss, _             = session.NewSession(aws.NewConfig().WithRegion("eu-central-1"))
	db                = dynamodb.New(ss)
	pocketTokenTable  = os.Getenv("POCKET_TOKEN_TABLE")
	pocketGetCodeUrl  = "https://getpocket.com/v3/oauth/request"
	pocketAddUrl      = "https://getpocket.com/v3/add"
	pocketGetTokenUrl = "https://getpocket.com/v3/oauth/authorize"
	pocketConsumerKey = os.Getenv("POCKET_CONSUMER_KEY")
	pocketRedirectUri = os.Getenv("TG_REDIRECT_URL")
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
	var sendUrl *url.URL
	sendUrl, err := url.Parse(tgUrl)
	if err != nil {
		log.Println("Error parsing url", err)
	}
	sendUrl.Path += "bot" + os.Getenv("TG_TOKEN") + "/" + "sendMessage"
	parameters := url.Values{}
	parameters.Add("text", text)
	parameters.Add("chat_id", chatId)
	sendUrl.RawQuery = parameters.Encode()
	result, _ := http.Get(sendUrl.String())
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
	body := request.Body
	log.Println("Received message = ", body)
	message := parseMessage(body)
	processMessage(message)
	return events.APIGatewayProxyResponse{StatusCode: 200, Headers: responseHeaders}, nil
}

func processMessage(message Message) {
	chatId := strconv.Itoa(message.Chat.Id)
	text := message.Text
	user := message.User
	var e error
	chat := message.ForwardFromChat
	if text == "/start" {
		e = createPocketApiToken(user, chatId)
	} else if text == "/authorize" {
		e = createUserCode(user, chatId)
	} else if chat.Type == "channel" {
		addToPocketFromChannel(chat, message, user, chatId)
	} else {
		sendMessage(text, chatId)
	}
	if e != nil {
		log.Println("Error = ", e)
	}
}

func addToPocketFromChannel(chat Chat, message Message, user User, chatId string) {
	messageLink := getMessageLinkInChannel(chat, message.ForwardFromMessageId)
	log.Println("messageLink = ", messageLink)
	userToken, e := getUserToken(user)
	if e != nil {
		log.Println("Error on getting user token", e)
	}
	e = addToPocket(userToken.Token, messageLink, chat.Title)
	if e == nil {
		sendMessage("Added successfully", chatId)
	}
}

func addToPocket(userToken string, messageLink string, title string) error {
	addBody, _ := json.Marshal(map[string]string{
		"url":          messageLink,
		"title":        title,
		"consumer_key": pocketConsumerKey,
		"access_token": userToken,
	})

	req, err := http.NewRequest("POST", pocketAddUrl, bytes.NewBuffer(addBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return errors.New("error response on adding item")
	}
	return nil

}

func getMessageLinkInChannel(chat Chat, messageId int) string {
	return "https://t.me/" + chat.Username + "/" + strconv.Itoa(messageId) + "?embed=1&userpic=true"
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
	sendMessage(fmt.Sprintf(pocketAuthUrl, token)+" Authorize you pocket via this url, "+
		"then you will be redirected back to bot and "+
		"need to press start, if redirection fails, then just send /start manually", chatId)
}

type UserToken struct {
	Id    string `json:"id"`
	Token string `json:"token"`
	Code  string `json:"code"`
}

func createPocketApiToken(user User, chatId string) error {
	userToken, err := getUserToken(user)
	if err != nil {
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

type UserCodeResponse struct {
	Code  string `json:"code"`
	State string `json:"state"`
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

	log.Println("Response body:", string(body))

	var f = UserCodeResponse{}
	err = json.Unmarshal(body, &f)
	if err != nil {
		return "", err
	}
	return f.Code, nil
}

type UserTokenResponse struct {
	AccessToken string `json:"access_token"`
	Username    string `json:"username"`
}

func createToken(code string) (string, error) {
	authBody, _ := json.Marshal(map[string]string{
		"consumer_key": pocketConsumerKey,
		"code":         code,
	})

	req, err := http.NewRequest("POST", pocketGetTokenUrl, bytes.NewBuffer(authBody))
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

	log.Println("Response body:", string(body))
	log.Println("Error:", resp.Header.Get("X-Error"))

	var f = UserTokenResponse{}
	err = json.Unmarshal(body, &f)
	if err != nil {
		return "", err
	}
	return f.AccessToken, nil
}

func getUserToken(user User) (*UserToken, error) {
	input := &dynamodb.GetItemInput{
		TableName: aws.String(pocketTokenTable),
		Key: map[string]*dynamodb.AttributeValue{
			"id": {
				S: aws.String(strconv.Itoa(user.Id)),
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
