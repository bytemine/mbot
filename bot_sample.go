// Copyright (c) 2016 Mattermost, Inc. All Rights Reserved.
// See License.txt for license information.

package main

import (
	"os"
	"os/signal"
	"regexp"
	"strings"

	"github.com/gorilla/mux"
	"github.com/spf13/viper"
	"github.com/mattermost/platform/model"
	"log"
	"fmt"
	"net/http"
)

type BotConfig struct {
	MattermostServer string
	MattermostWSURL string
	Listen string
	BotName string
	UserEmail string
	UserName string
	UserPassword string
	UserLastname string
	UserFirstname string
	TeamName string
	LogChannel string
	MainChannel string
	StatusChannel string
}

var client *model.Client4
var webSocketClient *model.WebSocketClient

var botUser *model.User
var botTeam *model.Team
var debuggingChannel *model.Channel
var mainChannel *model.Channel
var statusChannel *model.Channel

var config BotConfig

// Documentation for the Go driver can be found
// at https://godoc.org/github.com/mattermost/platform/model#Client
func main() {

	viper.SetConfigName("app")
	viper.AddConfigPath("config")

	err := viper.ReadInConfig()
	if err != nil {
		log.Fatal("Config file not found or error parsing\n\n: %s", err)
	} else {
		config.MattermostServer = viper.GetString("general.mattermost")
		config.MattermostWSURL = viper.GetString("general.wsurl")
		config.Listen     = viper.GetString("general.listen")
		config.BotName  = viper.GetString("general.botname")
		config.UserEmail = viper.GetString("general.useremail")
		config.UserName = viper.GetString("general.username")
		config.UserPassword = viper.GetString("general.userpassword")
		config.UserLastname = viper.GetString("general.userlastname")
		config.UserFirstname = viper.GetString("general.userfirstname")
		config.TeamName = viper.GetString("general.teamname")
		config.LogChannel = viper.GetString("channel.log")
		config.MainChannel = viper.GetString("channel.main")
		config.StatusChannel = viper.GetString("channel.status")

		fmt.Printf("\nUsing config:\n mattermost = %s\n" +
			"Log Channel = %s\n" +
			"username = %s\n" +
			"Listening on port: %s\n",
			config.MattermostServer,
			config.LogChannel,
			config.UserName,
		    config.Listen)
	}


	SetupGracefulShutdown()

	router := mux.NewRouter()
	router.HandleFunc("/sip/{action}/{user}/{number}", HandleSip)
	go func() { log.Fatal(http.ListenAndServe(config.Listen, router))}()


	client = model.NewAPIv4Client(config.MattermostServer)

	// Lets test to see if the mattermost server is up and running
	MakeSureServerIsRunning()

	// lets attempt to login to the Mattermost server as the bot user
	// This will set the token required for all future calls
	// You can get this token with client.AuthToken
	LoginAsTheBotUser()

	// If the bot user doesn't have the correct information lets update his profile
	UpdateTheBotUserIfNeeded()

	// Lets find our bot team
	FindBotTeam()

	// This is an important step.  Lets make sure we use the botTeam
	// for all future web service requests that require a team.
	//client.SetTeamId(botTeam.Id)

	// Lets create a bot channel for logging debug messages into
	CreateBotDebuggingChannelIfNeeded()
	SendMsgToDebuggingChannel("_"+config.BotName+" has **started** running_", "")

	// Join to main channel
	JoinMainChannel()

	// Join statuschannel
	JoinStatusChannel()

	// Lets start listening to some channels via the websocket!
	webSocketClient, err := model.NewWebSocketClient(config.MattermostWSURL, client.AuthToken)
	if err != nil {
		println("We failed to connect to the web socket")
		//PrintError(err)
	}

	webSocketClient.Listen()

	go func() {
		for {
			select {
			case resp := <-webSocketClient.EventChannel:
				HandleWebSocketResponse(resp)
			}
		}
	}()

	// You can block forever with
	select {}
}

func HandleSip(rw http.ResponseWriter, req *http.Request) {

	log.Printf("Request from: %s", req.RemoteAddr)

	// /offhock/fkr
	vars := mux.Vars(req)
	action := vars["action"]
	user := vars["user"]

	var text string
	var channel string

	switch action {
	case "offhook":
		text = fmt.Sprintf("%s CONNECTED", user)
		channel = statusChannel.Id
	case "onhook":
		text = fmt.Sprintf("%s DISCONNECTED", user)
		channel = statusChannel.Id
	case "dnd-on":
		text = fmt.Sprintf("%s DND", user)
		channel = statusChannel.Id
	case "dnd-off":
		text = fmt.Sprintf("%s available", user)
		channel = statusChannel.Id
	case "paused-on":
		text = fmt.Sprintf("%s paused", user)
		channel = statusChannel.Id
	case "paused-off":
		text = fmt.Sprintf("%s is back", user)
		channel = statusChannel.Id
	case "login":
		text = fmt.Sprintf("%s logged on", user)
		channel = statusChannel.Id
	case "logout":
		text = fmt.Sprintf("%s logged out", user)
		channel = statusChannel.Id
	case "agent-login":
		text = fmt.Sprintf("%s logged in", user)
		channel = statusChannel.Id
	case "agent-logout":
		text = fmt.Sprintf("%s logged out", user)
		channel = statusChannel.Id
	case "incoming-call":
		number := vars["number"]
		text = fmt.Sprintf("Incoming call for %s from %s", user, number)
		channel = mainChannel.Id
	case "incoming-conf":
		number := vars["number"]
		text = fmt.Sprintf("Incoming call for conference %s from %s", user, number)
		channel = mainChannel.Id
	}

	SendMsgToChannel(text, "", channel)

	fmt.Fprintln(rw, string(""))
}

func MakeSureServerIsRunning() {
	if props, resp := client.GetOldClientConfig(""); resp.Error != nil {
		println("There was a problem pinging the Mattermost server.  Are you sure it's running?")
		PrintError(resp.Error)
		os.Exit(1)
	} else {
		println("Server detected and is running version " + props["Version"])
	}
}

func LoginAsTheBotUser() {
	if user, resp := client.Login(config.UserEmail, config.UserPassword); resp.Error != nil {
		println("There was a problem logging into the Mattermost server.  Are you sure ran the setup steps from the README.md?")
		PrintError(resp.Error)
		os.Exit(1)
	} else {
		botUser = user
	}
}

func UpdateTheBotUserIfNeeded() {
	if botUser.FirstName != config.UserFirstname || botUser.LastName != config.UserLastname || botUser.Username != config.UserName {
		botUser.FirstName = config.UserFirstname
		botUser.LastName = config.UserLastname
		botUser.Username = config.UserName

		if user, resp := client.UpdateUser(botUser); resp.Error != nil {
			println("We failed to update the Sample Bot user")
			PrintError(resp.Error)
			os.Exit(1)
		} else {
			botUser = user
			println("Looks like this might be the first run so we've updated the bots account settings")
		}
	}
}

func FindBotTeam() {
	if team, resp := client.GetTeamByName(config.TeamName, ""); resp.Error != nil {
		println("We failed to get the initial load")
		println("or we do not appear to be a member of the team '" + config.TeamName + "'")
		PrintError(resp.Error)
		os.Exit(1)
	} else {
		botTeam = team
	}
}

func CreateBotDebuggingChannelIfNeeded() {
	if rchannel, resp := client.GetChannelByName(config.LogChannel, botTeam.Id, ""); resp.Error != nil {
		println("We failed to get the channels")
		PrintError(resp.Error)
	} else {
		debuggingChannel = rchannel
		return
	}

	// Looks like we need to create the logging channel
	channel := &model.Channel{}
	channel.Name = config.LogChannel
	channel.DisplayName = "Debugging For Sample Bot"
	channel.Purpose = "This is used as a test channel for logging bot debug messages"
	channel.Type = model.CHANNEL_OPEN
	channel.TeamId = botTeam.Id
	if rchannel, resp := client.CreateChannel(channel); resp.Error != nil {
		println("We failed to create the channel " + config.LogChannel)
		PrintError(resp.Error)
	} else {
		debuggingChannel = rchannel
		println("Looks like this might be the first run so we've created the channel " + config.LogChannel)
	}
}

func JoinMainChannel() {
	if rchannel, resp := client.GetChannelByName(config.MainChannel, botTeam.Id, ""); resp.Error != nil {
		println("We failed to get the channels")
		PrintError(resp.Error)
	} else {
		mainChannel = rchannel
		return
	}
}

func JoinStatusChannel() {
	if rchannel, resp := client.GetChannelByName(config.StatusChannel, botTeam.Id, ""); resp.Error != nil {
		println("We failed to get the channels")
		PrintError(resp.Error)
	} else {
		statusChannel = rchannel
		return
	}
}

func SendMsgToChannel(msg string, replyToId string, channelId string) {
	post := &model.Post{}
	post.ChannelId = channelId
	post.Message = msg

	post.RootId = replyToId

	if _, resp := client.CreatePost(post); resp.Error != nil {
		SendMsgToDebuggingChannel("We failed to send a message to the main channel", "")
	}
}

func SendMsgToDebuggingChannel(msg string, replyToId string) {
	post := &model.Post{}
	post.ChannelId = debuggingChannel.Id
	post.Message = msg

	post.RootId = replyToId

	if _, resp := client.CreatePost(post); resp.Error != nil {
		println("We failed to send a message to the logging channel")
		PrintError(resp.Error)
	}
}

func HandleWebSocketResponse(event *model.WebSocketEvent) {
	HandleMsgFromDebuggingChannel(event)
}

func HandleMsgFromDebuggingChannel(event *model.WebSocketEvent) {
	// If this isn't the debugging channel then lets ingore it
	if event.Broadcast.ChannelId != debuggingChannel.Id {
		return
	}

	// Lets only reponded to messaged posted events
	if event.Event != model.WEBSOCKET_EVENT_POSTED {
		return
	}

	println("responding to debugging channel msg")

	post := model.PostFromJson(strings.NewReader(event.Data["post"].(string)))
	if post != nil {

		// ignore my events
		if post.UserId == botUser.Id {
			return
		}

		// if you see any word matching 'alive' then respond
		if matched, _ := regexp.MatchString(`(?:^|\W)alive(?:$|\W)`, post.Message); matched {
			SendMsgToDebuggingChannel("Yes I'm running", post.Id)
			return
		}

		// if you see any word matching 'up' then respond
		if matched, _ := regexp.MatchString(`(?:^|\W)up(?:$|\W)`, post.Message); matched {
			SendMsgToDebuggingChannel("Yes I'm running", post.Id)
			return
		}

		// if you see any word matching 'running' then respond
		if matched, _ := regexp.MatchString(`(?:^|\W)running(?:$|\W)`, post.Message); matched {
			SendMsgToDebuggingChannel("Yes I'm running", post.Id)
			return
		}

		// if you see any word matching 'hello' then respond
		if matched, _ := regexp.MatchString(`(?:^|\W)hello(?:$|\W)`, post.Message); matched {
			SendMsgToDebuggingChannel("Yes I'm running", post.Id)
			return
		}
	}

	SendMsgToDebuggingChannel("I did not understand you!", post.Id)
}

func PrintError(err *model.AppError) {
	println("\tError Details:")
	println("\t\t" + err.Message)
	println("\t\t" + err.Id)
	println("\t\t" + err.DetailedError)
}

func SetupGracefulShutdown() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for _ = range c {
			if webSocketClient != nil {
				webSocketClient.Close()
			}

			SendMsgToDebuggingChannel("_"+config.BotName+" has **stopped** running_", "")
			os.Exit(0)
		}
	}()
}
