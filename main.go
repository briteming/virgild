/*MIT License

Copyright (c) 2018 Станислав (swork91@mail.ru)

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE. */

package main

import (
	"flag"
	"net"
	"os"
	"runtime/debug"

	log "github.com/sirupsen/logrus"
	"gopkg.in/gcfg.v1"

	"github.com/swork9/virgild/models"
	"github.com/swork9/virgild/socks"
)

var (
	config *models.Config
)

func init() {
	configPtr := flag.String("c", "virgild.conf", "Config file to use")
	detachPtr := flag.Bool("d", false, "Daemonize service and detach from tty")
	flag.Parse()

	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})

	config = &models.Config{}
	err := gcfg.ReadFileInto(config, *configPtr)
	if err != nil {
		log.Fatalln("(config)", err)
	}
	if len(config.Server.LogFile) > 0 {
		file, err := os.OpenFile(config.Server.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			log.Fatalln("(log)", err)
		}

		log.SetOutput(file)
	}
	if config.Server.LogLevel == "debug" {
		log.SetLevel(log.DebugLevel)
	} else if config.Server.LogLevel == "info" {
		log.SetLevel(log.InfoLevel)
	} else if config.Server.LogLevel == "warn" {
		log.SetLevel(log.WarnLevel)
	} else if config.Server.LogLevel == "error" {
		log.SetLevel(log.ErrorLevel)
	} else if config.Server.LogLevel == "fatal" {
		log.SetLevel(log.FatalLevel)
	} else {
		log.SetLevel(log.ErrorLevel)
	}

	if err := daemonize(os.Args, config.Server.UID, config.Server.GID, *detachPtr); err != nil {
		log.Fatalln("(daemonize)", err)
	}
}

func main() {
	if config.Server.AllowTCPBind {
		if config.Server.TCPBindPortsEnd-config.Server.TCPBindPortsStart < 0 {
			log.Fatalln("(tcp bind) you must setup at least 1 tcp port for binding.")
		}
		if len(config.Server.TCPBindAddr) == 0 {
			log.Fatalln("(tcp bind) you must setup your external ip (or hostname) for tcp binding.")
		}

		config.Server.TCPBindAddrIP = net.ParseIP(config.Server.TCPBindAddr)
		if config.Server.TCPBindAddrIP != nil {
			// Just fix to make sure, that tcpBindAddrIP have 4 bytes in net.IP slice.
			t := config.Server.TCPBindAddrIP.To4()
			if t != nil {
				config.Server.TCPBindAddrIP = t
			}
		} else {
			config.Server.TCPBindAddrIsHostname = true
			config.Server.TCPBindAddrHostname = config.Server.TCPBindAddr
		}
	}

	authMethods, err := config.GetAuthMethods()
	if err != nil {
		log.Fatalln("(auth)", err)
	}

	if len(authMethods) == 0 && !config.Server.AllowAnonymous {
		log.Fatalln("(auth) current configuration will not work, because anonymous login disabled and no other auth methods configured.")
	}

	socksServers := []*socks.Server{}
	if len(config.Server.Bind) > 0 {
		var err error
		var server *socks.Server
		if len(config.Server.PrivateKey) > 0 && len(config.Server.PublicKey) > 0 {
			/// If you want to generate self signed cert for server, use something like this: openssl req -x509 -newkey rsa:4096 -keyout private.key -out public.key -nodes -days 365
			server, err = socks.NewTLSServer(config, authMethods)
		} else {
			server, err = socks.NewServer(config, authMethods)
		}
		if err != nil {
			log.Fatalln("(socks server)", err)
		}
		err = server.Init()
		if err != nil {
			log.Fatalln("(socks server)", err)
		}
		socksServers = append(socksServers, server)
	}

	if len(socksServers) == 0 {
		log.Fatalln("(socks server) nothing to start, please configure [server] config section")
	}

	errc := make(chan error)
	for _, server := range socksServers {
		go func(server *socks.Server) {
			defer server.Close()
			defer func() {
				if r := recover(); r != nil {
					switch x := r.(type) {
					case error:
						log.Fatal("(panic) ", x.Error(), ":\n", string(debug.Stack()))
					default:
						log.Fatal("(panic) ", x, ":\n", string(debug.Stack()))
					}
				}
			}()

			errc <- server.Start()
		}(server)
	}

	for i := 0; i < len(socksServers); i++ {
		select {
		case err := <-errc:
			if err != nil {
				log.Fatalln("(socks server)", err)
			}
		}
	}

	log.Warn("Exiting... Have a nice day.")

}
