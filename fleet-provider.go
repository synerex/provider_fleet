package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	gosocketio "github.com/mtfelian/golang-socketio"
	"github.com/mtfelian/golang-socketio/transport"
	fleet "github.com/synerex/proto_fleet"
	pb "github.com/synerex/synerex_api"
	nodeapi "github.com/synerex/synerex_nodeapi"
	pbase "github.com/synerex/synerex_proto"
	sxutil "github.com/synerex/synerex_sxutil"
)

var (
	fmsrv           = flag.String("fmsrv", "wss://fm.synergic.mobi:8443/", "FleetManager Server")
	hostName        = flag.String("host", "", "fleet provider host name")
	nodesrv         = flag.String("nodesrv", "127.0.0.1:9990", "Node ID Server")
	idlist          []uint64
	spMap           map[uint64]*sxutil.SupplyOpts
	mu              sync.Mutex
	sxServerAddress string
)

type Channel struct {
	Channel string `json:"channel"`
}

type MyFleet struct {
	VehicleId  int                    `json:"vehicle_id"`
	Status     int                    `json:"status"`
	Coord      map[string]interface{} `json:"coord"`
	Angle      float32                `json:"angle"`
	Speed      int                    `json:"speed"`
	MyServices map[string]interface{} `json"services"`
	Demands    []int                  `json:"demands"`
}

type MyVehicle struct {
	vehicles []*MyFleet `json:"vehicles"`
}

type MyJson map[string]interface{}

func init() {
	idlist = make([]uint64, 0)
	spMap = make(map[uint64]*sxutil.SupplyOpts)
}

// callback for each Demand
func demandCallback(clt *sxutil.SXServiceClient, dm *pb.Demand) {
	// check if demand is match with my supply.
	log.Println("Got ride share demand callback")

	if dm.GetDemandName() == "" { // this is Select!
		log.Println("getSelect!")

		clt.Confirm(sxutil.IDType(dm.GetId()), sxutil.IDType(dm.GetTargetId()))  // dm targetid must be propose id.

	} else { // not select
		// select any ride share demand!
		// should check the type of ride..

		sp := &sxutil.SupplyOpts{
			Target: dm.GetId(),
			Name:   "RideShare by Taxi",
			JSON:   `{"Price":` + strconv.Itoa(1000) + `,"Distance": 5200, "Arrival": 300, "Destination": 500, "Position":{"Latitude":36.6, "Longitude":135}}`,
		} // set TargetID as Demand.Id (User will check by them)

		mu.Lock()
		pid := clt.ProposeSupply(sp)
		idlist = append(idlist, pid)
		spMap[pid] = sp
		mu.Unlock()
	}
}

func subscribeDemand(client *sxutil.SXServiceClient) {
	// goroutine!
	ctx := context.Background() //
	client.SubscribeDemand(ctx, demandCallback)
	// comes here if channel closed
	log.Printf("Server closed... on taxi provider")
}

func oldproposeSupply(client pb.SynerexClient, targetNum uint64) {
	dm := pb.Supply{Id: 200, SenderId: 555, TargetId: targetNum, ChannelType: pbase.RIDE_SHARE, SupplyName: "Taxi"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := client.ProposeSupply(ctx, &dm)
	if err != nil {
		log.Fatalf("%v.Propose Supply err %v", client, err)
	}
	log.Println(resp)

}

func handleMessage(client *sxutil.SXServiceClient, param interface{}) {

	var bmap map[string]interface{}
	bmap = param.(map[string]interface{})
	//	fmt.Printf("Length is %d\n",len(bmap))
	for _, v := range bmap["vehicles"].([]interface{}) {
		m, _ := v.(map[string]interface{})
		// Make Protobuf Message from JSON
		fleet := fleet.Fleet{
			VehicleId: int32(m["vehicle_id"].(float64)),
			Angle:     float32(m["angle"].(float64)),
			Speed:     int32(m["speed"].(float64)),
			Status:    int32(m["status"].(float64)),
			Coord: &fleet.Fleet_Coord{
				Lat: float32(m["coord"].([]interface{})[0].(float64)),
				Lon: float32(m["coord"].([]interface{})[1].(float64)),
			},
		}

		out, err := proto.Marshal(&fleet)
		if err == nil {
			cont := pb.Content{Entity: out}
			// Register supply
			smo := sxutil.SupplyOpts{
				Name:  "Fleet Supply",
				Cdata: &cont,
			}
			//			fmt.Printf("Res: %v",smo)
			_, nerr := client.NotifySupply(&smo)
			if nerr != nil { // connection failuer with current client
				// we need to ask to nodeidserv?
				// or just reconnect.
				newClient := sxutil.GrpcConnectServer(sxServerAddress)
				if newClient != nil {
					log.Printf("Reconnect Server %s\n", sxServerAddress)
					client.SXClient = newClient
				}
			}
		} else {
			log.Printf("PB Marshal Error!", err)
		}
	}
}

func publishSupplyFromFleetManager(client *sxutil.SXServiceClient, ch chan error) {
	// Connect by SocketIO
	fmt.Printf("Dial to  [%s]\n", *fmsrv)
	sioClient, err := gosocketio.Dial("wss://fm.synergic.mobi:8443/socket.io/?EIO=3&transport=websocket", transport.DefaultWebsocketTransport())
	if err != nil {
		log.Printf("SocketIO Dial error: %s", err)
		return
	}
	//	defer sioClient.Close()

	sioClient.On(gosocketio.OnConnection, func(c *gosocketio.Channel, param interface{}) {
		log.Printf("Fleet-Provider socket.io connected %v", c)
	})
	sioClient.On(gosocketio.OnDisconnection, func(c *gosocketio.Channel, param interface{}) {
		log.Printf("Fleet-Provider socket.io disconnected %v", c)
		ch <- fmt.Errorf("Disconnected!\n")
		// should connect again..
	})

	sioClient.On("vehicle_status", func(c *gosocketio.Channel, param interface{}) {
		//		fmt.Printf("Got %v",param)
		handleMessage(client, param)
		//		got := param.(string)
		//		fmt.Printf("Got %s\n",got)
	})

}

func runPublishSupplyInfinite(sclient *sxutil.SXServiceClient) {
	ch := make(chan error)
	for {
		publishSupplyFromFleetManager(sclient, ch)
		// wait for disconnected...
		res := <-ch
		if res == nil {
			break
		}
		time.Sleep(3 * time.Second)
	}
}

func main() {
	log.Printf("FleetProvider(%s) built %s sha1 %s", sxutil.GitVer, sxutil.BuildTime, sxutil.Sha1Ver)
	flag.Parse()
	go sxutil.HandleSigInt()
	sxutil.RegisterDeferFunction(sxutil.UnRegisterNode)

	channelTypes := []uint32{pbase.RIDE_SHARE}
	sxo := &sxutil.SxServerOpt{
		NodeType:   nodeapi.NodeType_PROVIDER,
		ServerInfo: *hostName,
		ClusterId:  0,
		AreaId:     "Default",
	}

	// obtain synerex server address from nodeserv
	srv, err := sxutil.RegisterNode(
		*nodesrv,
		"FleetProvider",
		channelTypes,
		sxo,
	)

	if err != nil {
		log.Fatal("Can't register node...")
	}
	log.Printf("Connecting Server [%s]\n", srv)

	wg := sync.WaitGroup{} // for syncing other goroutines
	sxServerAddress = srv
	client := sxutil.GrpcConnectServer(srv)
	argJson := fmt.Sprintf("{Client:Fleet}")
	sclient := sxutil.NewSXServiceClient(client, pbase.RIDE_SHARE, argJson)

	wg.Add(1)

	// We add Fleet Provider to "RIDE_SHARE" Supply

	/*	cont := pb.Content{Entity: []byte{0}}
		smo := sxutil.SupplyOpts{
			Name:  "Fleet Supply",
			Cdata: &cont,
		}
		sclient.NotifySupply(&smo)
	*/
	go runPublishSupplyInfinite(sclient)
	//	go subscribeDemand(sclient)
	wg.Wait()
	sxutil.CallDeferFunctions() // cleanup!

}
