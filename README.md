# mbot

This is `mbot`. A simple and fun way to interact with mattermost.

## mattermost bot - simple framework

This is a small framework to interact with mattermost. The bot itself won't
do anything - it needs plugins.
The bot does all the initialization and makes sure the plugins have a simple way
of interacting with mattermost. During plugin initialization the function _SetChannels_
is being called. Once that is done the plugin has pointers to the relevant mattermost
channels. For now `mbot` offers three channels: Main, Status and Log.

Example of outputting to the channel from within a plugin:

```
switch action {
case "offhook":
	text = fmt.Sprintf("%s CONNECTED", user)
	channel = dChannelId
case "onhook":
	text = fmt.Sprintf("%s DISCONNECTED", user)
	channel = dChannelId
}

mbothelper.SendMsgToChannel(text, "", channel)
```

## Configuration

`mbot` is configured through ``config/bot.toml``. `mbot` loads plugins.
Plugins are either _handler_ or _watcher_. _handler_ react to http requests and post
to channels, _watcher_ observe a channel and react to stuff written there.

### General section

```
[general]
mattermost = "https://mattermost.example.com"
wsurl = "wss://mattermost.example.com:443"
listen = ":5678"
botname = "Bender"
useremail = "bender@example.com"
username = "bender"
userpassword = "bender1234"
userlastname = "McSmithy"
userfirstname = "Bender"
teamname = "superteam"
plugins_directory = "plugins/"
plugins = "rtcrmapi_plugin.so sip_plugin.so"
```

Most items are self-explanatory, the channels are define in their own section
just as the `plugins` settings defines the shared objects to load.

```
[channel]
main = "town-square"
log = "debug"
status  = "status"
```

Each shared object has its own cateogory:

```
[sip_plugin.so]
handler = "HandleRequest"
watcher = "HandleChannelMessage"
mention_handler = "HandleMention"
help_handler = "HelpMe"
channels = "ExtraChannel"
path_patterns = "/sip/{action}/{user}/{number} /sip/{action}/{user}"
plugin_config = "sip_plugin.toml"
```

## Functions

A `mbot`-plugin can implement the following functions:

* The handler - referenced in the `handler`-setting.
* The watcher - referenced in the `watcher`-setting.
* The mention Handler - referenced in the `mention_handler`-setting.
* The help handler - referenced in the `help_handler`-setting.

A handler reacts to events from the outside (such as an http-request), while watcher observe
mattermost channels and react to certain messages, such as mentions.

### All Plugins

* LoadConfig(configFile string)
* SetChannels(mChannelIdString string, sChannelIdString string, dChannelIdString string)

### _handler_

* HandleRequest(rw http.ResponseWriter, req *http.Request)

### _watcher_

* HandleChannelMessage(event *model.WebSocketEvent, post *model.Post)

The watcher handler as access to the complete `event` (which is a [WebSocketEvent](https://github.com/mattermost/mattermost-server/blob/master/model/websocket_message.go)
from the mattermost model). To ease handling it
is being passed a pointer to the `post` (see [post](https://github.com/mattermost/mattermost-server/blob/master/model/post.go)) directly.

### _mention_handler_

* HandleMention(event *model.WebSocketEvent, post *model.Post)

The Mention handler as access to the complete `event` (which is a [WebSocketEvent](https://github.com/mattermost/mattermost-server/blob/master/model/websocket_message.go)
from the mattermost model). To ease handling it
is being passed a pointer to the `post` (see [post](https://github.com/mattermost/mattermost-server/blob/master/model/post.go)) directly.

## _help_handler_

* HandleHelp(userId string, message string)

The userId the user being that asked for help, with the contents of the
help inquery in the `message`.

### channels

In addtion to the debug, status as well as the lounge channel, each plugin can add addtional channels.
This is done via a space-seperated list in the `channels`-setting.

### Loading configs

Each plugin can have their own config file. _If_ `plugin_config` is defined in the
plugin section, this config file will be passed to `LoadConfigs` as a string, so that
the plugin can do whatever it wants with it.

