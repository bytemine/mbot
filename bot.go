// Copyright (c) 2016 Mattermost, Inc. All Rights Reserved.
// See License.txt for license information.

package main

import (
	"os"
	"plugin"

	"fmt"
	"github.com/bytemine/mbothelper"
	"github.com/mattermost/platform/model"
	"github.com/spf13/viper"
	"log"
)

var client *model.Client4
var webSocketClient *model.WebSocketClient

var botUser *model.User
var botTeam *model.Team
var debuggingChannel *model.Channel
var mainChannel *model.Channel
var statusChannel *model.Channel

var config mbothelper.BotConfig

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
		config.Listen = viper.GetString("general.listen")
		config.BotName = viper.GetString("general.botname")
		config.UserEmail = viper.GetString("general.useremail")
		config.UserName = viper.GetString("general.username")
		config.UserPassword = viper.GetString("general.userpassword")
		config.UserLastname = viper.GetString("general.userlastname")
		config.UserFirstname = viper.GetString("general.userfirstname")
		config.TeamName = viper.GetString("general.teamname")
		config.LogChannel = viper.GetString("channel.log")
		config.MainChannel = viper.GetString("channel.main")
		config.StatusChannel = viper.GetString("channel.status")

		fmt.Printf("\nUsing config:\n mattermost = %s\n"+
			"Log Channel = %s\n"+
			"username = :%s:\npassword = :%s:\n"+
			"Listening on port: %s\n",
			config.MattermostServer,
			config.LogChannel,
			config.UserName,
            config.UserPassword,
			config.Listen)
	}

	// load module
	// 1. open the so file to load the symbols
	plug, err := plugin.Open("rtcrm-plugin.so")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// 2. look up a symbol (an exported function or variable)
	// in this case, variable Greeter
	pluginHandler, err := plug.Lookup("HandleChannelMessage")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// 2. look up a symbol (an exported function or variable)
	// in this case, variable Greeter
	pluginHandlerSetChannels, err := plug.Lookup("SetChannelsAndClient")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// 2. look up a symbol (an exported function or variable)
	// in this case, variable Greeter
	//pathPattern, err := plug.Lookup("PathPattern")
	//if err != nil {
	//	fmt.Println(err)
	//	os.Exit(1)
	//}

	mbothelper.SetupGracefulShutdown()

	client = model.NewAPIv4Client(config.MattermostServer)

	mbothelper.InitMbotHelper(config, client)

	// Lets test to see if the mattermost server is up and running
	mbothelper.MakeSureServerIsRunning()

	// lets attempt to login to the Mattermost server as the bot user
	// This will set the token required for all future calls
	// You can get this token with client.AuthToken
	mbothelper.LoginAsTheBotUser()

	// If the bot user doesn't have the correct information lets update his profile
	mbothelper.UpdateTheBotUserIfNeeded()

	// Lets find our bot team
	mbothelper.FindBotTeam()

	// This is an important step.  Lets make sure we use the botTeam
	// for all future web service requests that require a team.
	//client.SetTeamId(botTeam.Id)

	// Lets create a bot channel for logging debug messages into
	mbothelper.CreateBotDebuggingChannelIfNeeded()
	mbothelper.SendMsgToDebuggingChannel("_"+config.BotName+" has **started** running_", "")

	// Join to main channel
	mainChannel = mbothelper.JoinChannel(config.MainChannel, mbothelper.BotTeam.Id)
	statusChannel = mbothelper.JoinChannel(config.StatusChannel, mbothelper.BotTeam.Id)

	// Lets start listening to some channels via the websocket!
	webSocketClient, err := model.NewWebSocketClient(config.MattermostWSURL, client.AuthToken)
	if err != nil {
		println("We failed to connect to the web socket")
		//PrintError(err)
	}

	webSocketClient.Listen()

	//pluginHandlerSetChannels.(func(string, string, *model.Client4))(mainChannel.Id, statusChannel.Id,client)
	pluginHandlerSetChannels.(func(string, *model.Client4))(mbothelper.DebuggingChannel.Id, client)

	//router := mux.NewRouter()
	//router.HandleFunc(*pathPattern.(*string), pluginHandler.(func(http.ResponseWriter, *http.Request)))
	//go func() { log.Fatal(http.ListenAndServe(config.Listen, router))}()

	go func() {
		for {
			select {
			case resp := <-webSocketClient.EventChannel:
				HandleWebSocketResponse(resp, pluginHandler)
			}
		}
	}()

	// You can block forever with
	select {}
}

func HandleWebSocketResponse(event *model.WebSocketEvent, pluginHandler plugin.Symbol) {
	pluginHandler.(func(socketEvent *model.WebSocketEvent))(event)
	//	HandleMsgFromDebuggingChannel(event)
}
