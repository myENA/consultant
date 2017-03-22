package consulCandidate

import (
	"github.com/hashicorp/consul/api"
	"time"
)

// MaxWait specifies the maximum wait after failed lock checks
const MaxWait = 10

// lockRunner runs the lock
func (c *Candidate) lockRunner() {
	var se *api.SessionEntry // session entry
	var kv *api.KVPair       // retrieved key
	var qm *api.QueryMeta    // metadata
	var qo *api.QueryOptions // options
	var err error            // error holder
	var ok bool              // lock status
	var checkWait int        // fail retry

	// build options
	qo = new(api.QueryOptions)
	qo.WaitIndex = uint64(0)
	qo.WaitTime = c.sessionTTL

	// increment wait group
	c.wait.Add(1)

	// cleanup on exit
	defer c.wait.Done()

	// main loop
	for {
		// attempt to get the key
		if kv, qm, err = c.client.KV().Get(c.kv.Key, qo); err != nil {
			// log warning
			logPrintf(c, "lockRunner() error checking lock: %s", err)
			// increment counter up to maximum value
			if checkWait < MaxWait {
				checkWait++
			}
			// log sleep
			if debug {
				logPrintf(c, "lockRunner() sleeping for %d seconds before retry ...", checkWait)
			}
			// sleep before retry
			time.Sleep(time.Duration(checkWait) * time.Second)
			/// next interation
			continue
		}

		// reset wait on success
		checkWait = 0

		// check closing
		if c.closing {
			if debug {
				logPrintln(c, "lockRunner() exiting")
			}
			return
		}

		// update index
		qo.WaitIndex = qm.LastIndex

		// check kv
		if kv != nil {
			if kv.Session == c.sessionID {
				// we are the leader
				c.updateLeader(true)
				continue
			}

			// still going ... check session
			if kv.Session != "" {
				// lock (probably) held by someone else...try to find out who it is
				se, _, err = c.client.Session().Info(kv.Session, nil)
				// check error
				if err != nil {
					// failed to get session - log error
					logPrintf(c, "lockRuner() error fetching session: %s", err)
					// renew/rebuild session
					c.sessionValidate()
					// wait for next iteration
					continue
				}
				// check returned session entry
				if se == nil {
					// nil session entry - nil kv and attempt to get the lock below
					kv = nil
				}
			} else {
				// missing session - nil kv and attempt to get the lock below
				kv = nil
			}
		}

		// not the leader
		c.updateLeader(false)

		// check for nil key
		if kv == nil {
			if debug {
				logPrintln(c, "lockRunner() nil lock or empty session detected - attempting to get lock ...")
			}
			// attempt to get the lock and check for error
			if ok, _, err = c.client.KV().Acquire(c.kv, nil); err != nil {
				logPrintf(c, "lockRunner() error failed to aquire lock: %s", err)
				// renew/rebuild session
				c.sessionValidate()
				// wait for next iteration
				continue
			}
			// we might have the lock
			if ok && err == nil {
				logPrintf(c, "lockRunner() acquired lock with session %s", c.sessionID)
				// yep .. we're the leader
				c.updateLeader(true)
				continue
			}
		}
	}
}
