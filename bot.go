/*************************************************************************
 * Written by / Copyright (C) 2017 bytemine GmbH                          *
 * Author: Felix Kronlage                   E-Mail: kronlage@bytemine.net *
 *                                                                        *
 * http://www.bytemine.net/                                               *
 *************************************************************************/

// this source was initially based on the mattermost sample bot
// https://github.com/mattermost/mattermost-bot-sample-golang

package main

import (
	"net/http"
	"os"
	"plugin"

	"fmt"
	"github.com/bytemine/mbothelper"
	"github.com/gorilla/mux"
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

func main() {

	// look in config/bot.toml for the config
	viper.SetConfigName("bot")
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
		config.PluginsDirectory = viper.GetString("general.plugins_directory")
		config.Plugins = viper.GetStringSlice("general.plugins")

		fmt.Printf("\nUsing config:\n\nmattermost = %s\n"+
			"Log Channel = %s\n"+
			"username = :%s\n"+
			"Listening on port: %s\n\n",
			config.MattermostServer,
			config.LogChannel,
			config.UserName,
			config.Listen)
	}

	// make sure we exit cleanly upon ctrl-c
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

	// Lets create a bot channel for logging debug messages into
	mbothelper.CreateBotDebuggingChannelIfNeeded()
	mbothelper.SendMsgToDebuggingChannel("_"+config.BotName+" has **started** running_", "")

	// Join to main channel...
	mbothelper.MainChannel = mbothelper.JoinChannel(config.MainChannel, mbothelper.BotTeam.Id)

	// ...and our status channel
	mbothelper.StatusChannel = mbothelper.JoinChannel(config.StatusChannel, mbothelper.BotTeam.Id)

	// Lets start listening to some channels via the websocket!
	webSocketClient, err := model.NewWebSocketClient(config.MattermostWSURL, client.AuthToken)
	if err != nil {
		println("We failed to connect to the web socket")
		//PrintError(err)
	}

	webSocketClient.Listen()

	// for request handler plugins
	router := mux.NewRouter()

	config.PluginsConfig = make(map[string]mbothelper.BotConfigPlugin)

	// iterate over plugins
	// each plugin will run in a goroutine
	for _, openPlugin := range config.Plugins {
		// load module
		// 1. open the so file to load the symbols
		plug, err := plugin.Open(config.PluginsDirectory + openPlugin)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		keyType := fmt.Sprintf("%s.type", openPlugin)
		keyHandler := fmt.Sprintf("%s.handler", openPlugin)
		pathPatterns := fmt.Sprintf("%s.path_patterns", openPlugin)
		pluginConfigFile := fmt.Sprintf("%s.config_file", openPlugin)

		pluginConfigFileName := ""
		if(viper.IsSet(pluginConfigFile)) {
			pluginConfigFileName = viper.GetString(pluginConfigFile)
		}

		pluginConfig := mbothelper.BotConfigPlugin{openPlugin, viper.GetString(keyType),
			viper.GetString(keyHandler), viper.GetStringSlice(pathPatterns),pluginConfigFileName}

		config.PluginsConfig[openPlugin] = pluginConfig


		mbothelper.SendMsgToDebuggingChannel(fmt.Sprintf("Loaded plugin: %s", openPlugin), "")

		// 2. look up a symbol (an exported function or variable)
		pluginHandler, err := plug.Lookup(pluginConfig.Handler)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		// 3. we always have a set channels function
		pluginHandlerSetChannels, err := plug.Lookup("SetChannels")
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		// if we have a configured config file for the plugin load it
		if pluginConfigFileName != "" {
			pluginConfigHandler, err := plug.Lookup("LoadConfig")
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

			mbothelper.SendMsgToDebuggingChannel(fmt.Sprintf("Loading configuration file '%s' for plugin: %s",
											pluginConfigFileName, openPlugin), "")

			pluginConfigHandler.(func (string))(pluginConfigFileName)
		}

		pluginHandlerSetChannels.(func(string, string, string))(mbothelper.MainChannel.Id, mbothelper.StatusChannel.Id, mbothelper.DebuggingChannel.Id)

		if pluginConfig.PluginType == "handler" || pluginConfig.PluginType == "all" {
			for _, pathPattern := range pluginConfig.PathPatterns {
				msg := fmt.Sprintf("Setting up routing for %s", pathPattern)
				mbothelper.SendMsgToDebuggingChannel(msg, "")
				router.HandleFunc(pathPattern, pluginHandler.(func(http.ResponseWriter, *http.Request)))
			}
			go func() { log.Fatal(http.ListenAndServe(config.Listen, router)) }()
		}


		if pluginConfig.PluginType == "watcher" || pluginConfig.PluginType == "all" {
			go func() {
				for {
					select {
					case resp := <-webSocketClient.EventChannel:
						HandleWebSocketResponse(resp, pluginHandler)
					}
				}
			}()
		}

		mbothelper.SendMsgToDebuggingChannel(fmt.Sprintf("Done initializing plugin: %s", openPlugin), "")

	}

	// You can block forever with
	select {}
}

func HandleWebSocketResponse(event *model.WebSocketEvent, pluginHandler plugin.Symbol) {
	pluginHandler.(func(socketEvent *model.WebSocketEvent))(event)
}
