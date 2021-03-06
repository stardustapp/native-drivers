package driver

import (
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/stardustapp/dustgo/lib/base"
	"github.com/stardustapp/dustgo/lib/extras"
	"github.com/stardustapp/dustgo/lib/inmem"
	"github.com/stardustapp/dustgo/lib/skylink"
	"github.com/stardustapp/dustgo/lib/toolbox"
)

var sessionCache map[string]*Session = make(map[string]*Session)

func (r *Root) OpenSessionImpl(chartUrl string) *Session {
	if session, ok := sessionCache[chartUrl]; ok {
		return session
	}

	apps := inmem.NewFolder("apps")
	session := &Session{
		Apps:     apps,
		ChartURL: chartUrl,
	}
	sessionCache[chartUrl] = session

	// Store a session reference
	if r.Sessions == nil {
		// TODO: this should be made already
		r.Sessions = inmem.NewObscuredFolder("sessions")
	}
	sessionId := extras.GenerateId()
	if ok := r.Sessions.Put(sessionId, session); !ok {
		log.Println("WARN: Session store rejected us :(")
		return nil
	}
	sessionPath := fmt.Sprintf(":9234/pub/sessions/%s", sessionId)
	session.URI, _ = toolbox.SelfURI(sessionPath)

	// perform async so the open can complete eagerly
	go func() {
		log.Println("Opening URL", chartUrl)
		skylink := openSkylink(chartUrl)
		skyNs := base.NewNamespace("tmp:/", skylink)
		session.ctx = base.NewRootContext(skyNs)

		if appFolder, ok := session.ctx.GetFolder("/apps"); ok {
			for _, appName := range appFolder.Children() {
				appFold, ok := session.ctx.GetFolder("/apps/" + appName)
				if !ok {
					// not an app
					continue
				}

				// Build a namespace for the app
				rootDir := inmem.NewFolderOf(appName,
					inmem.NewFolder("state"),
					inmem.NewFolder("export"))
				appNs := base.NewNamespace("skylink://skychart.local/~"+appName, rootDir)
				rootDir.Put("session", skylink) // the user's chart, i suppose
				rootDir.Put("source", appFold)  // source code

				// locate read-only config dir
				if fold, ok := session.ctx.GetFolder("/config/" + appName); ok {
					rootDir.Put("config", fold)
				} else {
					log.Println("Creating /config folder for", appName)
					session.ctx.Put("/config/"+appName, inmem.NewFolder(appName))
					if fold, ok := session.ctx.GetFolder("/config/" + appName); ok {
						rootDir.Put("config", fold)
					} else {
						log.Println("WARN: couldn't create /config/" + appName)
					}
				}

				// locate read-write data dir
				if fold, ok := session.ctx.GetFolder("/persist/" + appName); ok {
					rootDir.Put("persist", fold)
				} else {
					log.Println("Creating /persist folder for", appName)
					session.ctx.Put("/persist/"+appName, inmem.NewFolder(appName))
					if fold, ok := session.ctx.GetFolder("/persist/" + appName); ok {
						rootDir.Put("persist", fold)
					} else {
						log.Println("WARN: couldn't create /persist/" + appName)
					}
				}

				// register the app in the runtime
				log.Println("Adding app", appName)
				app := &App{
					AppName:   appName,
					Session:   session,
					Processes: inmem.NewFolder("processes"),
					Status:    "Ready", // TODO: not really, wait for the launch process?

					Namespace: rootDir,
					ctx:       base.NewRootContext(appNs),
				}
				apps.Put(appName, app)

				// launch every app at boot
				app.StartRoutineImpl(&ProcessParams{
					RoutineName: "launch",
				})
			}
		}
	}()

	log.Println("Returning session")
	return session
}

// Skylink helpers
// TODO: move to skylink package or something

func openWire(wireURI string) (base.Folder, bool) {
	uri, err := url.Parse(wireURI)
	if err != nil {
		log.Println("Skylink Wire URI parsing failed.", wireURI, err)
		return nil, false
	}

	skylink := openSkylink(uri.Scheme + "://" + uri.Host)
	if skylink == nil {
		return nil, false
	}
	skyNs := base.NewNamespace("tmp:/", skylink)
	skyCtx := base.NewRootContext(skyNs)

	return skyCtx.GetFolder(uri.Path)
}

func openSkylink(linkURI string) base.Entry {
	uri, err := url.Parse(linkURI)
	if err != nil {
		log.Println("Skylink URI parsing failed.", linkURI, err)
		return nil
	}

	log.Println("Importing", uri.Scheme, uri.Host)
	switch uri.Scheme {

	case "skylink+http", "skylink+https":
		actualUri := strings.TrimPrefix(linkURI, "skylink+") + "/~~export"
		return skylink.ImportUri(actualUri)

	case "skylink+ws", "skylink+wss":
		actualUri := strings.TrimPrefix(linkURI, "skylink+") + "/~~export/ws"
		return skylink.ImportUri(actualUri)

	case "skylink":
		names := strings.Split(uri.Host, ".")
		if len(names) == 3 && names[2] == "local" && names[1] == "chart" {

			// import the cluster-local skychart endpoint
			skychart := openSkylink("skylink+ws://skychart")
			skyNs := base.NewNamespace("skylink://skychart.local", skychart)
			skyCtx := base.NewRootContext(skyNs)

			invokeFunc, ok := skyCtx.GetFunction("/pub/open/invoke")
			if !ok {
				log.Println("Skychart didn't offer open function.")
				return nil
			}

			chartMeta := invokeFunc.Invoke(nil, inmem.NewString("", names[0]))
			if chartMeta == nil {
				log.Println("Skychart couldn't open", names[0], "chart")
				return nil
			}

			chartMetaF := chartMeta.(base.Folder)
			browseE, _ := chartMetaF.Fetch("browse")
			browseF := browseE.(base.Folder)
			invokeE, _ := browseF.Fetch("invoke")
			browseFunc := invokeE.(base.Function)

			chart := browseFunc.Invoke(nil, nil)
			if chart == nil {
				log.Println("Skychart couldn't browse", names[0], "chart")
				return nil
			}

			return chart

		} else {
			log.Println("Unknown skylink URI hostname", uri.Host)
			return nil
		}

	default:
		log.Println("Unknown skylink URI scheme", uri.Scheme)
		return nil
	}
}
