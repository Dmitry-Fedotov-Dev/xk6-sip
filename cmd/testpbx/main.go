// Command testpbx runs the test registrar/B2BUA for local k6 runs and
// writes the generated subscribers to a CSV file for the scripts.
//
//	testpbx -addr 127.0.0.1:5060 -users 200 -csv examples/subscribers.csv
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/Dmitry-Fedotov-Dev/xk6-sip/testpbx"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:5060", "UDP listen address")
	domain := flag.String("domain", "test.local", "SIP domain")
	n := flag.Int("users", 100, "number of subscribers user1..userN, extensions from -ext")
	firstExt := flag.Int("ext", 1001, "first extension")
	pass := flag.String("pass", "secret", "password of all subscribers")
	auth := flag.Bool("auth", true, "challenge REGISTER (401) and INVITE (407)")
	csvPath := flag.String("csv", "", "write subscribers to this CSV file")
	verbose := flag.Bool("v", false, "log SIP warnings")
	flag.Parse()

	users := make([]testpbx.User, *n)
	for i := range users {
		users[i] = testpbx.User{
			Name:     "user" + strconv.Itoa(i+1),
			Password: *pass,
			Ext:      strconv.Itoa(*firstExt + i),
		}
	}
	logger := slog.New(slog.DiscardHandler)
	if *verbose {
		logger = slog.Default()
	}
	pbx, err := testpbx.Start(testpbx.Config{
		Addr: *addr, Domain: *domain, Users: users,
		AuthRegister: *auth, AuthInvite: *auth, Logger: logger,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer pbx.Close()

	if *csvPath != "" {
		if err := writeCSV(*csvPath, pbx, users); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("testpbx on %s, domain %s, %d users (ext %d..%d)\n",
		pbx.Addr(), pbx.Domain(), *n, *firstExt, *firstExt+*n-1)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	var lastInv int64
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			inv := pbx.Stats.Invites.Load()
			if inv != lastInv {
				fmt.Printf("registers=%d invites=%d answered=%d\n",
					pbx.Stats.Registers.Load(), inv, pbx.Stats.Answered.Load())
				lastInv = inv
			}
		}
	}
}

func writeCSV(path string, pbx *testpbx.PBX, users []testpbx.User) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	w.Write([]string{"device", "registrar", "user", "pass", "expires", "ext"})
	for _, u := range users {
		w.Write([]string{"phone-" + u.Name, "sip:" + pbx.Addr(), u.Name + "@" + pbx.Domain(), u.Password, "300", u.Ext})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
