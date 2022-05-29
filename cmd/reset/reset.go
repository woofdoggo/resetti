package reset

import (
	"fmt"
	"os"
	"os/signal"
	"resetti/cfg"
	"resetti/manager"
	"resetti/mc"
	"resetti/ui"
	"resetti/x11"
	"syscall"
	"time"

	obs "github.com/woofdoggo/go-obs"
)

func run(mode string, mgr manager.Manager) {
	conf, err := cfg.GetConfig()
	if err != nil {
		fmt.Println("Failed to read config:", err)
		os.Exit(1)
	}
	if mode == "wall" && !conf.OBS.Enabled {
		fmt.Println("OBS integration must be enabled for wall.")
		fmt.Println("Please update your configuration.")
		os.Exit(1)
	}
	var o *obs.Client
	var obsErrors chan error
	if conf.OBS.Enabled {
		o = &obs.Client{}
		authRequired, errch, err := o.Connect(fmt.Sprintf("localhost:%d", conf.OBS.Port))
		if err != nil {
			fmt.Println("Failed to connect to OBS:", err)
			os.Exit(1)
		}
		obsErrors = errch
		if authRequired {
			err = o.Login(conf.OBS.Password)
			if err != nil {
				fmt.Println("Failed to authenticate with OBS:", err)
				os.Exit(1)
			}
		}
	}
	x, err := x11.NewClient()
	if err != nil {
		fmt.Println("Failed to connect to X server:", err)
		os.Exit(1)
	}
	x.Loop()
	instances, err := mc.GetInstances(x)
	if err != nil {
		fmt.Println("Failed to get Minecraft instances:", err)
		os.Exit(1)
	}
	u := ui.Ui{}
	u.Start(instances)
	mgr.SetConfig(*conf)
	mgr.SetDeps(x, o)
	mgrErrors := make(chan error)
	err = mgr.Start(instances, mgrErrors)
	if err != nil {
		fmt.Println("Failed to start manager:", err)
		return
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	for {
		select {
		case <-signals:
			ui.Log("Shutting down.")
			u.Stop()
			mgr.Stop()
			x.LoopStop()
			return
		case err := <-mgrErrors:
			ui.LogError("Fatal manger error: %s", err)
			mgr.Wait()
			ui.Log("Attempting to reboot manager...")
			time.Sleep(1 * time.Second)
			instances, err := mc.GetInstances(x)
			if err != nil {
				ui.LogError("Failed to get Minecraft instances: %s", err)
				u.Stop()
				x.LoopStop()
				return
			}
			err = mgr.Start(instances, mgrErrors)
			if err != nil {
				ui.LogError("Failed to restart manager: %s", err)
				u.Stop()
				x.LoopStop()
				return
			}
		case err := <-obsErrors:
			ui.LogError("OBS websocket error: %s", err)
		case err := <-x.Errors:
			if err == x11.ErrConnectionDied {
				ui.LogError("X connection died.")
				ui.Log("Shutting down.")
				u.Stop()
				mgr.Stop()
				return
			} else {
				ui.LogError("X error: %s", err)
			}
		}
	}
}