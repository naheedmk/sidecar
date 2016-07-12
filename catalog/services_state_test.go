package catalog

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/relistan/go-director"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/newrelic/sidecar/service"
)

var hostname = "shakespeare"
var anotherHostname = "chaucer"

func Test_NewServer(t *testing.T) {

	Convey("Invoking NewServer()", t, func() {
		Convey("Returns a server with the correct name", func() {
			server := NewServer(hostname)
			So(server.Name, ShouldEqual, hostname)
		})

		Convey("Initializes the map", func() {
			server := NewServer(hostname)
			So(server.Services, ShouldNotBeNil)
		})

		Convey("Initializes the time", func() {
			server := NewServer(hostname)
			So(server.LastUpdated, ShouldBeTheSameTimeAs, time.Unix(0, 0))
		})

		Convey("Initializes the LastChanged", func() {
			server := NewServer(hostname)
			So(server.LastChanged, ShouldBeTheSameTimeAs, time.Unix(0, 0))
		})
	})
}

func Test_NewServicesState(t *testing.T) {

	Convey("Invoking NewServicesState()", t, func() {

		Convey("Initializes the Servers map", func() {
			state := NewServicesState()
			So(state.Servers, ShouldNotBeNil)
		})

		Convey("Initializes LastChanged", func() {
			state := NewServicesState()
			So(state.LastChanged, ShouldBeTheSameTimeAs, time.Unix(0, 0))
		})

	})
}

func Test_ServicesStateWithData(t *testing.T) {

	Convey("When working with data", t, func() {
		state := NewServicesState()
		state.Servers[hostname] = NewServer(hostname)

		baseTime := time.Now().UTC()

		svcId := "deadbeef123"

		svc := service.Service{
			ID:       svcId,
			Name:     "radical_service",
			Image:    "101deadbeef",
			Created:  baseTime,
			Hostname: anotherHostname,
			Updated:  baseTime,
			Status:   service.ALIVE,
		}

		Convey("Encode() generates JSON that we can Decode()", func() {
			decoded, err := Decode(state.Encode())

			So(err, ShouldBeNil)
			So(decoded.Servers[hostname].Name, ShouldEqual, hostname)
			So(len(decoded.Servers), ShouldEqual, 1)
		})

		Convey("Decode() returns an error when handed junk", func() {
			result, err := Decode([]byte("asdf"))

			So(result.Servers, ShouldBeEmpty)
			So(err, ShouldNotBeNil)
		})

		Convey("HasServer() is true when a server exists", func() {
			So(state.HasServer(hostname), ShouldBeTrue)
		})

		Convey("HasServer() is false when a server is missing", func() {
			So(state.HasServer("junk"), ShouldBeFalse)
		})

		Convey("GetLocalService() returns a service when we have it", func() {
			state.Hostname = anotherHostname
			state.AddServiceEntry(svc)

			So(state.GetLocalService(svcId), ShouldResemble, &svc)
		})

		Convey("AddServiceEntry()", func() {
			Convey("Merges in a new service", func() {
				So(state.HasServer(anotherHostname), ShouldBeFalse)

				state.AddServiceEntry(svc)

				So(state.HasServer(anotherHostname), ShouldBeTrue)
				So(state.Servers[anotherHostname].Services[svc.ID], ShouldNotBeNil)
			})

			Convey("Doesn't merge a stale service", func() {
				state.AddServiceEntry(svc)

				staleService := service.Service{
					ID:       "deadbeef123",
					Name:     "stale_service",
					Image:    "stale",
					Created:  baseTime,
					Hostname: anotherHostname,
					Updated:  baseTime.Add(0 - 1*time.Minute),
					Status:   service.ALIVE,
				}

				state.AddServiceEntry(staleService)

				So(state.HasServer(anotherHostname), ShouldBeTrue)
				So(state.Servers[anotherHostname].Services[svc.ID].Updated,
					ShouldBeTheSameTimeAs, baseTime)
				So(state.Servers[anotherHostname].Services[svc.ID].Image,
					ShouldEqual, "101deadbeef")
			})

			Convey("Updates the LastUpdated time for the server", func() {
				newDate := svc.Updated.AddDate(0, 0, 5)
				svc.Updated = newDate
				state.AddServiceEntry(svc)

				So(state.Servers[anotherHostname].LastUpdated, ShouldBeTheSameTimeAs, newDate)
			})

			Convey("Updates the LastChanged time for a service when new", func() {
				lastChanged := state.LastChanged
				state.AddServiceEntry(svc)

				So(state.LastChanged.After(lastChanged), ShouldBeTrue)
				So(
					state.Servers[anotherHostname].LastChanged.After(lastChanged),
					ShouldBeTrue,
				)
			})

			Convey("Updates the LastChanged time for a service when changing", func() {
				state.AddServiceEntry(svc)
				lastChanged := state.LastChanged
				svc.Tombstone()
				state.AddServiceEntry(svc)

				So(state.LastChanged.After(lastChanged), ShouldBeTrue)
			})

			Convey("Skips LastChanged time for a service that didn't change", func() {
				state.AddServiceEntry(svc)
				lastChanged := state.LastChanged
				svc.Updated = time.Now().UTC()
				state.AddServiceEntry(svc)

				So(state.LastChanged.After(lastChanged), ShouldBeFalse)
			})

			Convey("Retransmits a packet when the state changes", func() {
				state.AddServiceEntry(svc)
				<-state.Broadcasts // Catch the retransmit from the initial add
				svc.Tombstone()
				state.AddServiceEntry(svc)

				packet := <-state.Broadcasts

				encoded, _ := svc.Encode()
				So(len(packet), ShouldEqual, 1)
				So(string(packet[0]), ShouldEqual, string(encoded))
			})

			Convey("Doesn't retransmit when the state is the same", func() {
				state.Broadcasts = make(chan [][]byte, 1)
				state.AddServiceEntry(svc)
				svc.Updated = svc.Updated.Add(1 * time.Second)
				state.AddServiceEntry(svc)

				pendingBroadcast := false
				select {
				case <-state.Broadcasts:
					pendingBroadcast = true
				default:
				}
				So(pendingBroadcast, ShouldBeFalse)
			})

			Convey("Doesn't retransmit an add of a new service for this host", func() {
				state.Hostname = hostname
				state.Broadcasts = make(chan [][]byte, 1)
				state.AddServiceEntry(svc)

				pendingBroadcast := false
				select {
				case <-state.Broadcasts:
					pendingBroadcast = true
				default:
				}
				So(pendingBroadcast, ShouldBeFalse)
			})
		})

		Convey("Merge() merges state we care about from other state structs", func() {
			firstState := NewServicesState()
			secondState := NewServicesState()
			firstState.AddServiceEntry(svc)
			secondState.Merge(firstState)

			So(len(secondState.Servers), ShouldEqual, len(firstState.Servers))
			So(secondState.Servers[svcId], ShouldEqual, firstState.Servers[svcId])
		})

		Convey("Format() pretty-prints the state even without a Memberlist", func() {
			formatted := state.Format(nil)

			So(formatted, ShouldNotBeNil)
		})

		Reset(func() {
			state = NewServicesState()
			state.Servers[hostname] = NewServer(hostname)
		})
	})
}

func Test_TrackingAndBroadcasting(t *testing.T) {

	Convey("When Tracking and Broadcasting services", t, func() {
		state := NewServicesState()
		state.Servers[hostname] = NewServer(hostname)
		svcId1 := "deadbeef123"
		svcId2 := "deadbeef101"
		baseTime := time.Now().UTC().Round(time.Second)

		service1 := service.Service{ID: svcId1, Hostname: hostname, Updated: baseTime}
		service2 := service.Service{ID: svcId2, Hostname: hostname, Updated: baseTime}
		services := []service.Service{service1, service2}

		containerFn := func() []service.Service {
			return services
		}

		state.Hostname = hostname
		state.tombstoneRetransmit = 1 * time.Nanosecond

		looper := director.NewFreeLooper(1, nil)

		Convey("The correct number of messages are sent", func() {
			looper := director.NewFreeLooper(5, make(chan error))
			state.Broadcasts = make(chan [][]byte, 5)
			state.SendServices(services, looper)
			looper.Wait()

			So(len(state.Broadcasts), ShouldEqual, 5)
		})

		Convey("All of the services are added to state", func() {
			looper := director.NewFreeLooper(1, make(chan error))

			go state.TrackNewServices(containerFn, looper)
			looper.Wait()

			So(state.Servers[hostname].Services[svcId1], ShouldNotBeNil)
			So(state.Servers[hostname].Services[svcId2], ShouldNotBeNil)
			So(state.Servers[hostname].Services[svcId1].ID, ShouldEqual, svcId1)
			So(state.Servers[hostname].Services[svcId2].ID, ShouldEqual, svcId2)
		})

		Convey("New services are serialized into the channel", func() {
			go state.BroadcastServices(containerFn, looper)

			json1, _ := json.Marshal(service1)
			json2, _ := json.Marshal(service2)

			readBroadcasts := <-state.Broadcasts
			So(len(readBroadcasts), ShouldEqual, 2)
			So(string(readBroadcasts[0]), ShouldEqual, string(json1))
			So(string(readBroadcasts[1]), ShouldEqual, string(json2))
		})

		Convey("Puts a nil into the broadcasts channel when no services", func() {
			emptyList := func() []service.Service { return []service.Service{} }
			go state.BroadcastServices(emptyList, looper)
			broadcast := <-state.Broadcasts

			So(broadcast, ShouldBeNil)
		})

		Convey("All of the tombstones are serialized into the channel", func() {
			junk := service.Service{ID: "runs", Hostname: hostname, Updated: baseTime}
			state.AddServiceEntry(junk)
			state.AddServiceEntry(service1)
			state.AddServiceEntry(service2)
			go state.BroadcastTombstones(containerFn, looper)

			readBroadcasts := <-state.Broadcasts
			So(len(readBroadcasts), ShouldEqual, 2) // 2 per service
			// Match with regexes since the timestamp changes during tombstoning
			So(readBroadcasts[0], ShouldMatch, "^{\"ID\":\"runs\".*\"Status\":1}$")
			So(readBroadcasts[1], ShouldMatch, "^{\"ID\":\"runs\".*\"Status\":1}$")
		})

		Convey("The timestamp is incremented on each subsequent service broadcast background run", func() {
			state.Broadcasts = make(chan [][]byte, 4)
			looper := director.NewFreeLooper(2, make(chan error))
			service1.Tombstone()
			service2.Tombstone()
			go state.SendServices([]service.Service{service1, service2}, looper)
			looper.Wait()

			// First go-round
			broadcasts := <-state.Broadcasts
			So(len(broadcasts), ShouldEqual, 2)
			// It's JSON so just string match rather than decoding
			So(broadcasts[0], ShouldMatch, service1.Updated.Format(time.RFC3339Nano))
			So(broadcasts[1], ShouldMatch, service2.Updated.Format(time.RFC3339Nano))

			// Second go-round
			broadcasts = <-state.Broadcasts
			So(len(broadcasts), ShouldEqual, 2)
			So(broadcasts[0], ShouldMatch, service1.Updated.Add(50*time.Nanosecond).Format(time.RFC3339Nano))
			So(broadcasts[1], ShouldMatch, service2.Updated.Add(50*time.Nanosecond).Format(time.RFC3339Nano))
		})

		Convey("The LastChanged time is changed when a service is Tombstoned", func() {
			lastChanged := state.LastChanged
			junk := service.Service{ID: "runs", Hostname: hostname, Updated: baseTime}
			state.AddServiceEntry(junk)
			go state.BroadcastTombstones(containerFn, looper)

			<-state.Broadcasts
			So(state.LastChanged.After(lastChanged), ShouldBeTrue)
			So(state.Servers[hostname].LastChanged.After(lastChanged), ShouldBeTrue)
		})

		Convey("Services that are still alive are not tombstoned", func() {
			state.AddServiceEntry(service1)
			state.AddServiceEntry(service2)
			go state.BroadcastTombstones(containerFn, looper)

			readBroadcasts := <-state.Broadcasts
			So(len(readBroadcasts), ShouldEqual, 0)
		})

		Convey("Puts a nil into the broadcasts channel when no tombstones", func() {
			emptyList := func() []service.Service { return []service.Service{} }
			go state.BroadcastTombstones(emptyList, looper)
			broadcast := <-state.Broadcasts

			So(broadcast, ShouldBeNil)
		})

		Convey("Tombstones have a lifespan, then expire", func() {
			service1.Tombstone()
			service1.Updated = service1.Updated.Add(0 - TOMBSTONE_LIFESPAN - 1*time.Minute)
			state.AddServiceEntry(service1)
			state.AddServiceEntry(service2)
			So(state.Servers[hostname].Services[service1.ID], ShouldNotBeNil)

			go state.BroadcastTombstones(containerFn, looper)
			<-state.Broadcasts

			So(state.Servers[hostname].Services[service1.ID], ShouldBeNil)
		})

		Convey("When the last tombstone is removed, so is the server", func() {
			state := NewServicesState() // Totally empty
			state.Hostname = hostname
			state.AddServiceEntry(service1)
			state.Servers[hostname].Services[service1.ID].Tombstone()
			state.Servers[hostname].Services[service1.ID].Updated =
				service1.Updated.Add(0 - TOMBSTONE_LIFESPAN - 1*time.Minute)

			So(state.Servers[hostname], ShouldNotBeNil)
			state.TombstoneOthersServices()
			So(state.Servers[hostname], ShouldBeNil)
		})

		Convey("Alive services have a lifespan and then are tombstoned", func() {
			lastChanged := state.Servers[hostname].LastChanged
			state.AddServiceEntry(service1)
			svc := state.Servers[hostname].Services[service1.ID]
			stamp := service1.Updated.Add(0 - ALIVE_LIFESPAN - 5*time.Second)
			svc.Updated = stamp

			state.TombstoneOthersServices()

			So(svc.Status, ShouldEqual, service.TOMBSTONE)
			So(svc.Updated, ShouldBeTheSameTimeAs, stamp.Add(time.Second))
			So(state.Servers[hostname].LastChanged.After(lastChanged), ShouldBeTrue)
		})

		Convey("Can detect new services or newly changed services", func() {
			// service1 and services[0] are copies of the same service
			service1.Status = service.UNHEALTHY
			services[0].Status = service.ALIVE
			state.AddServiceEntry(service1)

			So(state.IsNewService(&services[0]), ShouldBeTrue)
		})

		Convey("Doesn't call tombstones new services", func() {
			// service1 and services[0] are copies of the same service
			service1.Status = service.UNHEALTHY
			services[0].Status = service.TOMBSTONE
			state.AddServiceEntry(service1)

			So(state.IsNewService(&services[0]), ShouldBeFalse)
		})
	})
}

func Test_Listeners(t *testing.T) {
	Convey("Working with state Listeners", t, func() {
		state := NewServicesState()
		listener := make(chan ChangeEvent, 1)
		listener2 := make(chan ChangeEvent, 1)
		svcId1 := "deadbeef123"
		baseTime := time.Now().UTC().Round(time.Second)
		svc1 := service.Service{ID: svcId1, Hostname: hostname, Updated: baseTime}

		Convey("Adding listeners results in new entries in the listeners list", func() {
			So(len(state.listeners), ShouldEqual, 0)
			state.AddListener(listener)
			So(len(state.listeners), ShouldEqual, 1)
		})

		Convey("A major state change event notifies all listeners", func() {
			var result ChangeEvent
			var result2 ChangeEvent
			var wg sync.WaitGroup
			wg.Add(2)
			go func() { result = <-listener; wg.Done() }()
			go func() { result2 = <-listener2; wg.Done() }()
			state.AddListener(listener)
			state.AddListener(listener2)

			state.AddServiceEntry(svc1)

			svc1.Updated = svc1.Updated.Add(1 * time.Second)
			state.AddServiceEntry(svc1)

			wg.Wait()
			So(result.Service.Hostname, ShouldEqual, hostname)
			So(result2.Service.Hostname, ShouldEqual, hostname)
		})

		Reset(func() {
			state = NewServicesState()
		})
	})
}

func Test_ClusterMembershipManagement(t *testing.T) {

	Convey("When managing cluster members", t, func() {
		state := NewServicesState()
		state.Servers[hostname] = NewServer(hostname)
		svcId1 := "deadbeef123"
		svcId2 := "deadbeef101"
		baseTime := time.Now().UTC().Round(time.Second)

		service1 := service.Service{ID: svcId1, Hostname: hostname, Updated: baseTime}
		service2 := service.Service{ID: svcId2, Hostname: hostname, Updated: baseTime}

		state.Hostname = hostname
		state.tombstoneRetransmit = 1 * time.Nanosecond

		Convey("ExpireServer() tombstones all services for a server", func() {
			state.AddServiceEntry(service1)
			state.AddServiceEntry(service2)

			go state.ExpireServer(hostname)
			expired := <-state.Broadcasts

			So(len(expired), ShouldEqual, 2)
			// Timestamps chagne when tombstoning, so regex match
			So(expired[0], ShouldMatch, "^{\"ID\":\"deadbeef.*\"Status\":1}$")
			So(expired[1], ShouldMatch, "^{\"ID\":\"deadbeef.*\"Status\":1}$")
		})

		Convey("The state LastChanged is updated", func() {
			lastChanged := state.LastChanged
			state.AddServiceEntry(service1)
			state.AddServiceEntry(service2)
			go state.ExpireServer(hostname)

			<-state.Broadcasts
			So(lastChanged.Before(state.LastChanged), ShouldBeTrue)
		})

	})
}

func Example_ByServiceWithoutMatcher() {
	state := NewServicesState()
	state.Servers[hostname] = NewServer(hostname)
	svcId1 := "deadbeef123"
	svcId2 := "deadbeef101"
	svcId3 := "deadbeef105"
	baseTime := time.Now().UTC().Round(time.Second)

	service1 := service.Service{
		ID: svcId1, Name: "service1", Image: "img1",
		Hostname: hostname, Updated: baseTime,
	}
	service2 := service.Service{
		ID: svcId2, Name: "service2", Image: "img1",
		Hostname: hostname, Updated: baseTime,
	}
	service3 := service.Service{
		ID: svcId3, Name: "service3", Image: "img2",
		Hostname: hostname, Updated: baseTime,
	}
	state.AddServiceEntry(service1)
	state.AddServiceEntry(service2)
	state.AddServiceEntry(service3)

	json, _ := json.MarshalIndent(state.ByService(), "", "  ")
	println(string(json))
	// Output:
}

func Example_ByServiceWithMatcher() {
	state := NewServicesState()
	state.Servers[hostname] = NewServer(hostname)
	svcId1 := "deadbeef123"
	svcId2 := "deadbeef101"
	svcId3 := "deadbeef105"
	baseTime := time.Now().UTC().Round(time.Second)

	service1 := service.Service{
		ID: svcId1, Name: "service1-deadabba999", Image: "img1",
		Hostname: hostname, Updated: baseTime,
	}
	service2 := service.Service{
		ID: svcId2, Name: "service1-abba1231234", Image: "img1",
		Hostname: hostname, Updated: baseTime,
	}
	service3 := service.Service{
		ID: svcId3, Name: "service3", Image: "img2",
		Hostname: hostname, Updated: baseTime,
	}
	state.AddServiceEntry(service1)
	state.AddServiceEntry(service2)
	state.AddServiceEntry(service3)

	json, _ := json.MarshalIndent(state.ByService(), "", "  ")
	println(string(json))
	// Output:
}

func Example_BroadcastTombstones() {
	state := NewServicesState()
	state.Hostname = "something"

	looper := director.NewTimedLooper(1, 1*time.Nanosecond, nil)

	go func() { <-state.Broadcasts }()
	state.BroadcastTombstones(func() []service.Service { return []service.Service{} }, looper)

	// TODO go test seems broken. It should match this, but can't for some reason:
	// XXX it can't see output generated _by_ the test code itself
	// TombstoneServices(): New host or not running services, skipping.
	// Output:
}

func ShouldBeTheSameTimeAs(actual interface{}, expected ...interface{}) string {
	wanted := expected[0].(time.Time)
	got := actual.(time.Time)

	if !got.Equal(wanted) {
		return "expected:\n" + fmt.Sprintf("%#v", wanted) + "\n\ngot:\n" + fmt.Sprintf("%#v", got)
	}

	return ""
}

func ShouldMatch(actual interface{}, expected ...interface{}) string {
	wanted := expected[0].(string)
	got := actual.([]byte)

	wantedRegexp := regexp.MustCompile(wanted)

	if !wantedRegexp.Match(got) {
		return "expected:\n" + fmt.Sprintf("%#v", wanted) + "\n\nto match:\n" + fmt.Sprintf("%v", string(got))
	}

	return ""
}
