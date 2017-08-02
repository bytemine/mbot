# mbot

This is mbot. A simple and fun way to interact with mattermost.

## mattermost bot - simple framework

This is a small framework to interact with mattermost. The bot itself won't
do anything it needs plugins.

## Configuration

`mbot` is configured through ``config/bot.toml``. `mbot` loads plugins.
Plugins are either _handler_ or _watcher_. _handler_ react to http requests and post
to channels, _watcher_ observe a channel and react to stuff written there.

### General section

```
[general]
mattermost = "https://mattermost.bytemine.net"
wsurl = "wss://mattermost.bytemine.net:443"
listen = ":5020"
botname = "James"
useremail = "support@bytemine.net"
username = "james"
userpassword = "choo7Ohk5cohDeu"
userlastname = "Sophie"
userfirstname = "James"
teamname = "bytemine"
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
type = "handler"
handler = "HandleRequest"
path_patterns = "/sip/{action}/{user}/{number} /sip/{action}/{user}"
plugin_confog = "sip_plugin.toml"
```

## Functions

An `mbot`-plugin implemennts the following functions:

* The handler - referenced in the `handler`-setting in each plugin configuration setting.

### All Plugins

* LoadConfig(configFile string)
* SetChannels(mChannelIdString string, sChannelIdString string, dChannelIdString string)

### _handler_

* HandleRequest(rw http.ResponseWriter, req *http.Request)

### _watcher_

* HandleChannelMessage(event *model.WebSocketEvent)

### Loading configs

Each plugin can have their own config file. _If_ `plugin_config` is defined in the
plugin section, this config file will be passed to `LoadConfigs` as a string, so that
the plugin can do whatever it wants with it.

