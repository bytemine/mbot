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
	"plugin"

	"fmt"
	"log"
	"reflect"
	"unsafe"

	"encoding/json"
	"strings"

	"github.com/bytemine/mbothelper"
	"github.com/gorilla/mux"
	"github.com/mattermost/platform/model"
	"github.com/spf13/viper"
)

var client *model.Client4
var webSocketClient *model.WebSocketClient

var botUser *model.User
var botTeam *model.Team
var debuggingChannel *model.Channel
var mainChannel *model.Channel
var statusChannel *model.Channel

const MbotVersion = "0.0.1"

var config mbothelper.BotConfig

var helpHandlers map[string]plugin.Symbol

func main() {

	log.SetPrefix("[mbot] - ")
	log.SetFlags(log.Ldate | log.Lmicroseconds | log.Llongfile)

	// look in config/mbot.toml for the config
	viper.SetConfigName("mbot")
	viper.AddConfigPath("config")

	err := viper.ReadInConfig()
	if err != nil {
		log.Fatalf("Config file not found or error parsing: %v", err)
	}

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

	log.Printf("Using Config:\n%+v", config)

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
	mbothelper.MainChannel = mbothelper.JoinChannel(config.MainChannel, mbothelper.BotTeam.Id, mbothelper.BotUser.Id)

	// ...and our status channel
	mbothelper.StatusChannel = mbothelper.JoinChannel(config.StatusChannel, mbothelper.BotTeam.Id, mbothelper.BotUser.Id)

	helpHandlers = map[string]plugin.Symbol{}

	// for request handler plugins
	router := mux.NewRouter()

	config.PluginsConfig = make(map[string]mbothelper.BotConfigPlugin)

	// iterate over plugins
	// each plugin will run in a goroutine
	for _, openPlugin := range config.Plugins {
		// loading the plugin itself - open the so file to load the symbols
		plug, err := plugin.Open(config.PluginsDirectory + openPlugin)
		if err != nil {
			log.Printf("Plugin %v failed to load: %v", openPlugin, err)
			continue
		}

		inspectPlugin(plug)

		keyHandler := fmt.Sprintf("%s.handler", openPlugin)
		keyWatcher := fmt.Sprintf("%s.watcher", openPlugin)
		keyMentionHandler := fmt.Sprintf("%s.mention_handler", openPlugin)
		keyHelpHandler := fmt.Sprintf("%s.help_handler", openPlugin)
		pathPatterns := fmt.Sprintf("%s.path_patterns", openPlugin)
		pluginConfigFile := fmt.Sprintf("%s.config_file", openPlugin)
		channels := fmt.Sprintf("%s.channels", openPlugin)

		pluginConfigFileName := ""
		if viper.IsSet(pluginConfigFile) {
			pluginConfigFileName = viper.GetString(pluginConfigFile)
		}

		channelList := viper.GetStringSlice(channels)

		pluginConfig := mbothelper.BotConfigPlugin{
			PluginName:     openPlugin,
			Handler:        viper.GetString(keyHandler),
			Watcher:        viper.GetString(keyWatcher),
			MentionHandler: viper.GetString(keyMentionHandler),
			HelpHandler:    viper.GetString(keyHelpHandler),
			PathPatterns:   viper.GetStringSlice(pathPatterns),
			PluginConfig:   pluginConfigFileName,
			Channels:       make(map[string]*model.Channel),
		}

		config.PluginsConfig[openPlugin] = pluginConfig

		mbothelper.SendMsgToDebuggingChannel(fmt.Sprintf("Loaded plugin: %s", openPlugin), "")

		// we always have a set channels function
		pluginHandlerSetChannels, err := plug.Lookup("SetChannels")
		if err != nil {
			log.Printf("Symbol 'SetChannels' missing from plugin '%v'. Error: %v", openPlugin, err)
			continue
		}

		// if we have a configured config file for the plugin load it
		if pluginConfigFileName != "" {
			pluginConfigHandler, err := plug.Lookup("LoadConfig")
			if err != nil {
				log.Printf("Config file '%v' for plugin '%v' failed to process: %v", pluginConfigFileName, openPlugin, err)
				continue
			}

			mbothelper.SendMsgToDebuggingChannel(fmt.Sprintf("Loading configuration file '%s' for plugin: %s",
				pluginConfigFileName, openPlugin), "")

			pluginConfigHandler.(func(string))(pluginConfigFileName)
		}

		pluginHandlerSetChannels.(func(string, string, string))(mbothelper.MainChannel.Id, mbothelper.StatusChannel.Id, mbothelper.DebuggingChannel.Id)

		// join configured channels for plugin
		for _, channel := range channelList {
			log.Printf("joining channel: %s\n", channel)
			rchannel := mbothelper.JoinChannel(channel, mbothelper.BotTeam.Id, mbothelper.BotUser.Id)
			pluginConfig.Channels[channel] = rchannel
			mbothelper.SendMsgToDebuggingChannel(fmt.Sprintf("Joined channel '%s'", channel), "")
		}

		// look up a symbol (an exported function or variable)
		if pluginConfig.Handler != "" {
			pluginHandler, err := plug.Lookup(pluginConfig.Handler)
			if err != nil {
				log.Printf("Couldn't lookup handler: %v", err)
				continue
			}
			for _, pathPattern := range pluginConfig.PathPatterns {
				msg := fmt.Sprintf("Setting up routing for %s", pathPattern)
				mbothelper.SendMsgToDebuggingChannel(msg, "")
				router.HandleFunc(pathPattern, pluginHandler.(func(http.ResponseWriter, *http.Request)))
			}
			go func() { log.Fatal(http.ListenAndServe(config.Listen, router)) }()
		}

		if pluginConfig.Watcher != "" {
			pluginWatcher, err := plug.Lookup(pluginConfig.Watcher)
			if err != nil {
				log.Printf("Couldn't lookup watcher: %v", err)
				continue
			}

			mbothelper.SendMsgToDebuggingChannel(fmt.Sprintf("Registering watcher for plugin '%s'", openPlugin), "")

			go func() {

				// create a dedicated websocket client for this go routine
				webSocketClient, err := model.NewWebSocketClient(config.MattermostWSURL, client.AuthToken)
				if err != nil {
					log.Printf("Failed to connect to the web socket: %v", err)
				}

				webSocketClient.Listen()

				for resp := range webSocketClient.EventChannel {
					handleWebSocketResponse(resp, pluginWatcher)
				}
			}()
		}

		if pluginConfig.MentionHandler != "" {
			pluginMentionHandler, err := plug.Lookup(pluginConfig.MentionHandler)
			if err != nil {
				log.Printf("Couldn't lookup mention handler: %v", err)
				continue
			}

			mbothelper.SendMsgToDebuggingChannel(fmt.Sprintf("Registering mention_handler for plugin '%s'", openPlugin), "")

			go func() {

				// create a dedicated websocket client for this go routine
				webSocketClient, err := model.NewWebSocketClient(config.MattermostWSURL, client.AuthToken)
				if err != nil {
					log.Printf("Failed to connect to the web socket: %v", err)
				}

				webSocketClient.Listen()

				for resp := range webSocketClient.EventChannel {
					handleMention(resp, pluginMentionHandler)
				}
			}()
		}

		if pluginConfig.HelpHandler != "" {
			pluginHelpHandler, err := plug.Lookup(pluginConfig.HelpHandler)

			if err != nil {
				log.Printf("Couldn't lookup help handler: %v", err)
			}

			helpHandlers[pluginConfig.PluginName] = pluginHelpHandler

		}

		mbothelper.SendMsgToDebuggingChannel(fmt.Sprintf("Done initializing plugin: %s", openPlugin), "")

	}

	// block
	select {}
}

func handleWebSocketResponse(event *model.WebSocketEvent, pluginWatcher plugin.Symbol) {
	s, ok := event.Data["post"].(string)
	if !ok {
		log.Printf("handleWebSocketResponse: type assertion failed, want: string, got: %T", event.Data["post"])
		return
	}

	m := extractPost(s)
	pluginWatcher.(func(socketEvent *model.WebSocketEvent, Post *model.Post))(event, m)
}

func handleMention(event *model.WebSocketEvent, pluginMentionHandler plugin.Symbol) {
	sMention, ok := event.Data["mentions"].(string)
	if !ok {
		log.Printf("handleMention: type assertion failed, want: string, got: %T", event.Data["mentions"])
		return
	}

	if strings.Contains(sMention, mbothelper.BotUser.Id) {

		sPost, ok := event.Data["post"].(string)
		if !ok {
			log.Printf("handleMention: type assertion failed, want: string, got: %T", event.Data["post"])
			return
		}

		m := extractPost(sPost)

		// if we see 'help' in the message contents
		if strings.Contains(m.Message, "help") {
			handleHelp(m.UserId, m.Message)
		}

		pluginMentionHandler.(func(socketEvent *model.WebSocketEvent, Post *model.Post))(event, m)
	}
}

type Plug struct {
	Path    string
	_       chan struct{}
	Symbols map[string]interface{}
}

func extractPost(eventData string) *model.Post {

	j := []uint8(eventData)

	var m model.Post
	err := json.Unmarshal(j, &m)

	if err != nil {
		log.Printf("Error decoding json: %+v", err)
	}

	return &m
}

func handleHelp(userId string, message string) {

	helped := false

	// see if one of our plugins has a handler for this
	for pluginKey := range helpHandlers {

		if strings.Contains(message, pluginKey) {
			helpHandlers[pluginKey].(func(userId string, message string))(userId, message)
			helped = true
			break
		}
	}

	if !helped {
		// nope, not the case, fire general help
		help(userId)
	}
}

func help(userId string) {
	m := fmt.Sprintf("help - mbot - version: %s\nUse `help <pluginname>` for plugn specific help.\n", MbotVersion)

	for plugin := range helpHandlers {
		m = fmt.Sprintf("%s\n\t%s", m, plugin)
	}

	mbothelper.ReplyToUser(m, userId)

}

func inspectPlugin(p *plugin.Plugin) {
	pl := (*Plug)(unsafe.Pointer(p))

	log.Printf("Plugin %s exported symbols (%d):", pl.Path, len(pl.Symbols))

	for name, pointers := range pl.Symbols {
		log.Printf("symbol: %s, pointer: %v, type: %v", name, pointers, reflect.TypeOf(pointers))
	}
}
