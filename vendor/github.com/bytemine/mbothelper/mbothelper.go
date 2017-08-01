package mbothelper

import (
	"os"
	"os/signal"
	"regexp"
	"strings"

	"github.com/mattermost/platform/model"
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
	PluginsDirectory string
	Plugins []string
	PluginsConfig map[string]BotConfigPlugin
}

type BotConfigPlugin struct {
	PluginName string
	PluginType string
	Handler string
	PathPatterns []string
	PluginConfig string
}

var config BotConfig

var client *model.Client4
var webSocketClient *model.WebSocketClient

var botUser *model.User
var BotTeam *model.Team
var DebuggingChannel *model.Channel
var MainChannel *model.Channel
var StatusChannel *model.Channel

func InitMbotHelper(botConfig BotConfig, client4 *model.Client4) {
	config = botConfig
	client = client4
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
		BotTeam = team
	}
}

func CreateBotDebuggingChannelIfNeeded() {
	if rchannel, resp := client.GetChannelByName(config.LogChannel, BotTeam.Id, ""); resp.Error != nil {
		println("We failed to get the channels")
		PrintError(resp.Error)
	} else {
		DebuggingChannel = rchannel
		return
	}

	// Looks like we need to create the logging channel
	channel := &model.Channel{}
	channel.Name = config.LogChannel
	channel.DisplayName = "Debugging For Sample Bot"
	channel.Purpose = "This is used as a test channel for logging bot debug messages"
	channel.Type = model.CHANNEL_OPEN
	channel.TeamId = BotTeam.Id
	if rchannel, resp := client.CreateChannel(channel); resp.Error != nil {
		println("We failed to create the channel " + config.LogChannel)
		PrintError(resp.Error)
	} else {
		DebuggingChannel = rchannel
		println("Looks like this might be the first run so we've created the channel " + config.LogChannel)
	}
}

func JoinChannel(channel string, teamId string) (*model.Channel) {
	if rchannel, resp := client.GetChannelByName(channel, teamId, ""); resp.Error != nil {
		println("We failed to get the channels")
		PrintError(resp.Error)
	} else {
		return rchannel
	}
	return nil
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
	post.ChannelId = DebuggingChannel.Id
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
	if event.Broadcast.ChannelId != DebuggingChannel.Id {
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
