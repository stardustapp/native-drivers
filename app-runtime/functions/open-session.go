import "net/url"
import "log"
import "strings"
import "github.com/stardustapp/core/base"
import "github.com/stardustapp/core/inmem"
import "github.com/stardustapp/core/skylink"

func (r *Root) OpenSessionImpl(chartUrl string) *Session {
  apps := inmem.NewFolder("apps")
  session := &Session{
    Apps: apps,
  }

  // perform async so the open can complete eagerly
  go func() {
    log.Println("Opening URL", chartUrl)
    skylink := openSkylink(chartUrl)
    skyNs := base.NewNamespace("tmp:/", skylink)
    session.ctx = base.NewRootContext(skyNs)

    if appFolder, ok := session.ctx.GetFolder("/apps"); ok {
      for _, appName := range appFolder.Children() {
        log.Println("Adding app", appName)
        app := &App{
          AppName: appName,
          Session: session,
          Processes: inmem.NewFolder("processes"),
        }
        apps.Put(appName, app)

        // launch every app
        app.StartRoutineImpl("launch")
      }
    }
  }()

  log.Println("Returning session")
  return session
}

/*
func openWire(wireURI string) base.Folder {
  uri, err := url.Parse(wireURI)
  if err != nil {
    log.Println("Skylink Wire URI parsing failed.", wireURI, err)
    return nil
  }

  skylink := openSkylink(uri.Scheme + "://" + uri.Host)
  if skylink == nil {
    return nil
  }
  skyNs := base.NewNamespace("tmp:/", skylink)
  skyCtx := base.NewRootContext(skyNs)

  subPath, _ := skyCtx.GetFolder(uri.Path)
  return subPath
}
*/

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