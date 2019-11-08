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
	"net/url"
	"os"
	"strconv"
	"strings"
)

var (
	botName           = "SaveToPocketBot"
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
	authInfoMessage   = "Send /start to start authorization or send /stop to stop using bot."
	noItemsMessage    = "No items found, this bot accepts messages from channels or links."
)

type Body struct {
	Message Message `json:"message"`
}

type Message struct {
	User                 User            `json:"from"`
	ForwardFromUser      User            `json:"forward_from"`
	Chat                 Chat            `json:"chat"`
	ForwardFromChat      Chat            `json:"forward_from_chat"`
	ForwardFromMessageId int             `json:"forward_from_message_id"`
	Text                 string          `json:"text"`
	Entities             []MessageEntity `json:"entities"`
}

type MessageEntity struct {
	Offset int    `json:"offset"`
	Length int    `json:"length"`
	Type   string `json:"type"`
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
	} else if text == "/stop" {
		e = removePocketApiToken(user, chatId)
	} else if chat.Type == "channel" {
		e = addToPocketFromChannel(chat, message, user, chatId)
	} else if len(message.Entities) > 0 {
		e = addToPocketFromLinks(message.Entities, message.Text, user, chatId)
	} else {
		sendMessage(noItemsMessage, chatId)
	}
	if e != nil {
		processAddError(e, chatId)
	}
}

func removePocketApiToken(user User, chatId string) error {
	input := &dynamodb.DeleteItemInput{
		TableName: aws.String(pocketTokenTable),
		Key: map[string]*dynamodb.AttributeValue{
			"id": {
				S: aws.String(strconv.Itoa(user.Id)),
			},
		},
	}
	_, err := db.DeleteItem(input)
	if err == nil {
		sendMessage("You were successfully unauthorized", chatId)
	}
	return err
}

func processAddError(e error, chatId string) {
	if e == nil {
		return
	}
	switch e.(type) {
	case *BadResponseError:
		sendMessage("Pocket sent a response with error. Maybe a problem with authorization. "+authInfoMessage, chatId)
	case *NoItemsFoundError:
		sendMessage(noItemsMessage, chatId)
	case *NoUserTokenError:
		sendMessage("You need to be authorized. "+authInfoMessage, chatId)
	default:
		sendMessage("Unknown error.", chatId)
	}
}

func filterUrls(entities []MessageEntity, text string) (out []string) {
	urls := map[string]bool{}
	for _, entity := range entities {
		if entity.Type == "url" {
			urls[text[entity.Offset:entity.Offset+entity.Length]] = true
		}
	}
	for link := range urls {
		out = append(out, link)
	}
	return
}

func addToPocketFromLinks(entities []MessageEntity, text string, user User, chatId string) error {
	urls := filterUrls(entities, text)
	if len(urls) == 0 {
		return &NoItemsFoundError{}
	}
	userToken, e := getUserToken(user)
	if e != nil {
		log.Println("Error on getting user token", e)
		return e
	}
	for _, link := range urls {
		log.Println("Adding link = ", link)
		e = addToPocket(userToken.Token, link, []string{botName})
		if e != nil {
			return e
		}
		sendMessage(link+" was added successfully", chatId)
	}
	return nil
}

type NoItemsFoundError struct{}

func (e *NoItemsFoundError) Error() string {
	return fmt.Sprintf("Not user token")
}

type NoUserTokenError struct{}

func (e *NoUserTokenError) Error() string {
	return fmt.Sprintf("No user token")
}

type BadResponseError struct {
	Code int
}

func (e *BadResponseError) Error() string {
	return fmt.Sprintf("Bad response code: %d", e.Code)
}

func addToPocketFromChannel(chat Chat, message Message, user User, chatId string) error {
	messageLink := getMessageLinkInChannel(chat, message.ForwardFromMessageId)
	log.Println("messageLink = ", messageLink)
	userToken, e := getUserToken(user)
	if e != nil {
		log.Println("Error on getting user token", e)
		return e
	}
	e = addToPocketWithTitle(userToken.Token, messageLink, []string{chat.Title, botName}, chat.Title)
	if e == nil {
		sendMessage(messageLink+" was added successfully", chatId)
	}
	return e
}

func addToPocket(userToken string, messageLink string, tags []string) error {
	return addToPocketWithTitle(userToken, messageLink, tags, "")
}

func addToPocketWithTitle(userToken string, messageLink string, tags []string, title string) error {
	properties := map[string]string{
		"url":          messageLink,
		"consumer_key": pocketConsumerKey,
		"access_token": userToken,
	}
	if len(title) > 0 {
		properties["title"] = title
	}
	if len(tags) > 0 {
		properties["tags"] = strings.Join(tags, ",")
	}
	addBody, _ := json.Marshal(properties)

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
	log.Println("Error:", resp.Header.Get("X-Error"))
	if resp.StatusCode != 200 {
		return &BadResponseError{resp.StatusCode}
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
		sendMessage("You have already been authorized, ", chatId)
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

	if resp.StatusCode != 200 {
		return "", &BadResponseError{resp.StatusCode}
	}

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
		return nil, &NoUserTokenError{}
	}
	resultItem := tokenResult.Item
	if resultItem == nil {
		return nil, &NoUserTokenError{}
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
