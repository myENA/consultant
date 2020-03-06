package main

import (
	stdlog "log"
	"os"
	"os/signal"
	"syscall"
	"time"

	consulapi "github.com/hashicorp/consul/api"
	"github.com/myENA/consultant/v2"
)

const (
	lovePort        = 620
	loveCheckID     = "love-keepalive"
	loveCheckOutput = "I love you 💖"
	loveCheckNotes  = "💘💝💖💗💓💞💕💟❣❤🧡💛💚💙💜🤎🖤🤍"
)

func checkit(log *stdlog.Logger, client *consultant.Client, stop <-chan struct{}) {
	timer := time.NewTimer(10 * time.Second)

	for {
		select {
		case <-stop:
			if !timer.Stop() && len(timer.C) > 0 {
				<-timer.C
			}
			log.Printf("I kept love alive as long as I could :)")
			return

		case tick := <-timer.C:
			log.Printf("The time is %v and I'm working to keep love alive...", tick.Format(time.Kitchen))
			if err := client.Agent().UpdateTTL(loveCheckID, loveCheckOutput, consulapi.HealthPassing); err != nil {
				log.Printf("I was unable to keep love alive: %v", err)
			}
			timer.Reset(10 * time.Second)
		}
	}
}

func run(log *stdlog.Logger, stop <-chan struct{}, errc chan<- error) {
	var (
		client *consultant.Client
		msr    *consultant.ManagedAgentServiceRegistration
		ms     *consultant.ManagedService

		err error
	)

	if client, err = consultant.NewDefaultClient(); err != nil {
		errc <- err
		return
	}

	msr = consultant.NewBareManagedAgentServiceRegistration("love", lovePort)
	msr.AddTTLCheck(consulapi.HealthPassing, 30*time.Second, func(check *consulapi.AgentServiceCheck) {
		check.CheckID = loveCheckID
		check.Body = loveCheckOutput
		check.Notes = loveCheckNotes
	})

	log.Println("Adding more love to the world...")

	cfg := consultant.ManagedServiceConfig{
		Logger: stdlog.New(os.Stdout, "💟💟💟 -> ", stdlog.Lmsgprefix|stdlog.LstdFlags),
		Debug:  true,
		Client: client.Client,
	}

	if ms, err = msr.Create(&cfg); err != nil {
		errc <- err
		return
	}

	go checkit(stdlog.New(os.Stdout, "💋💋💋 -> ", stdlog.Lmsgprefix|stdlog.LstdFlags), client, stop)

	log.Println("Love has increased by one")

	<-stop

	log.Println("Stopping the love...")

	errc <- ms.Deregister()
}

func main() {
	var (
		sig os.Signal
		err error

		log = stdlog.New(os.Stdout, "❤ -> ", stdlog.Lmsgprefix|stdlog.LstdFlags)

		sigc = make(chan os.Signal, 1)
		stop = make(chan struct{}, 1)
		errc = make(chan error, 5)
	)

	defer close(sigc)
	defer close(errc)

	log.Println("Warming up")

	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)

	go run(log, stop, errc)

	select {
	case err = <-errc:
		log.SetPrefix("💔 -> ")
		log.Printf("I'm heartbroken: %v", err)
		os.Exit(1)
	case sig = <-sigc:
		log.SetPrefix("❣ -> ")
		log.Printf("Ok love, I'll stop: %v", sig)
	}

	close(stop)

	err = <-errc
	if err != nil {
		log.SetPrefix("💔 -> ")
		log.Printf("I'm heartbroken: %v", err)
		os.Exit(1)
	}

	log.Println("Goodbye")
}
