package consultant

import (
	"github.com/hashicorp/consul/api"
	"time"
)

// sessionValidate renews/recreates the session as needed
func (c *Candidate) sessionValidate() {
	var se *api.SessionEntry // session object
	var sid string           // temp session id
	var err error            // error holder

	// attempt to renew session
	if se, _, err = c.client.Session().Renew(c.sessionID, nil); err != nil || se == nil {
		// check error
		if err != nil {
			// log error
			logPrintf(c, "sessionValidate() failed to renew session: %s", err)
			// destroy sesion
			c.client.Session().Destroy(c.sessionID, nil)
		}
		// check session
		if se == nil {
			// log error
			logPrintln(c, "sessionValidate() failed to renew session: not found")
		}
		// recreate the session
		if sid, _, err = c.client.Session().Create(c.sessionEntry, nil); err != nil {
			logPrintf(c, "sessionValidate() failed to rebuild session: %s", err)
			return
		}
		// update session and lock pair
		c.lock.Lock()
		c.sessionID = sid
		c.kv.Session = sid
		c.lock.Unlock()
		// log session rebuild
		if debug {
			logPrintf(c, "sessionValidate() registered new session %s", sid)
		}
		// all done
		return
	}
	// renew okay
	// log.Debug("sessionValidate() renewed session: %s", c.sid)
}

// sessionKeepAlive keeps session and ttl check alive
func (c *Candidate) sessionKeepAlive() {
	var sleepDuration time.Duration // sleep duration
	var sleepTicker *time.Ticker    // sleep timer
	var err error                   // error holder

	// increment wait group
	c.wait.Add(1)

	// cleanup on exit
	defer c.wait.Done()

	// ensure sleep always at least one second
	if sleepDuration = c.sessionTTL / 2; sleepDuration < time.Second {
		sleepDuration = time.Second
	}

	// init ticker channel
	sleepTicker = time.NewTicker(sleepDuration)

	// loop every "sleep" seconds
	for range sleepTicker.C {
		// check closing
		if c.closing {
			if debug {
				logPrintln(c, "sessionKeepAlive() exiting")
			}
			// destroy session
			if _, err = c.client.Session().Destroy(c.sessionID, nil); err != nil {
				logPrintf(c, "sessionKeepAlive() failed to destroy session (%s) %s",
					c.sessionID,
					err)
			}
			// stop ticker and exit
			sleepTicker.Stop()
			return
		}
		// renew/rebuild session
		c.sessionValidate()
	}

	// shouldn't ever happen
	if !c.closing {
		logPrintln(c, "sessionKeepAlive() exiting unexpectedly")
	}
}
