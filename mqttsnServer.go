package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func random(min, max int) int {
	return rand.Intn(max-min) + min
}

type m2a struct{
	mac string
	addr *net.UDPAddr
}

func updateMacMap(macStr2Addr map[string]*net.UDPAddr, update chan *m2a) {
	for {
		ud := <- update
		fmt.Println("-------------------- updateMacMap --------------------")
		fmt.Println(ud.mac)
		fmt.Println(ud.addr)
		macStr2Addr[ud.mac] = ud.addr
		fmt.Println("-------------------- updateMacMap : End--------------------")
	}
}

func handleMqttSNPacket(connection *net.UDPConn, quit chan struct{}, macStr2Addr *map[string]*net.UDPAddr, update chan *m2a) {

	buffer := make([]byte, 1024)
	n, remoteAddr, err := 0, new(net.UDPAddr), error(nil)

	// mqtt client for workers
	var hdrHeartBeat mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {

		fmt.Println(" ++++++++++ HeartBeat Ack ++++++++++ ")
		fmt.Println("Heart Beat Ack got")
		//hb := &HB.HeartBeat{}
		//proto.Unmarshal(msg.Payload(), hb)

		msgTypeByte := byte(0x0c)
		flagByte := byte(0x62)
		tidBytes := make([]byte, 2)
		tidBytes[0] = byte('H')
		tidBytes[1] = byte('B')
		midBytes := make([]byte, 2)
		binary.BigEndian.PutUint16(midBytes, 0x0b00)
		lenByte := byte(1 + 1 + 1 + 2 + 2 + binary.Size(msg.Payload()))

		packet := make([]byte, lenByte)
		(packet)[0] = lenByte
		(packet)[1] = msgTypeByte
		(packet)[2] = flagByte
		copy((packet)[3:5], tidBytes)
		copy((packet)[5:7], midBytes)
		copy((packet)[7:], msg.Payload())

		macstring := hex.EncodeToString(msg.Payload()[2 : 2+6])
		fmt.Printf("Mac String %v\n", macstring)
		udpAddr := (*macStr2Addr)[macstring]
		fmt.Printf("Udp addr %+v\n", udpAddr)

		_, err = connection.WriteToUDP(buffer[0:n], udpAddr)
		fmt.Println("Sending back HeartBeat Ack")
		if err != nil {
			fmt.Printf("Error when re-sending : %v \n", err.Error())
			//quit <- struct{}{}
		}
		fmt.Println(" ++++++++++ HeartBeat Ack ++++++++++ ")
	}

	opts := mqtt.NewClientOptions().
		AddBroker("tcp://localhost:43518").
		SetClientID(fmt.Sprintf("mqtt-benchmark-%v-%v", time.Now().Format(time.RFC3339Nano), "MQTTSN-Gateway-worker")).
		SetCleanSession(true).
		SetAutoReconnect(true).
		SetOnConnectHandler(func(client mqtt.Client) {
			if token := client.Subscribe("HeartBeatAck", 0, hdrHeartBeat); token.Wait() && token.Error() != nil {
				fmt.Println(token.Error())
				quit <- struct{}{}
			} else {
				fmt.Println("Subscribe topic " + "HeartBeatAck" + " success\n")
			}

		}).
		SetConnectionLostHandler(func(client mqtt.Client, reason error) {
			fmt.Printf("CLIENT %v lost connection to the broker: %v. Will reconnect...\n", "MQTTSN-Gateway", reason.Error())
		})
	client := mqtt.NewClient(opts)

	token := client.Connect()
	token.Wait()
	if token.Error() != nil {
		fmt.Printf("CLIENT %v had error connecting to the broker: %v\n", "MQTTSN-Gateway", token.Error())
		quit <- struct{}{}
	}

	for err == nil {
		fmt.Println(" ++++++++++ Receiving HeartBeat Ack ++++++++++ ")

		n, remoteAddr, err = connection.ReadFromUDP(buffer)

		fmt.Printf("New message got")

		var topic string
		if buffer[3] == 0x42 {
			topic = "BLELocation"
		} else if buffer[3] == 0x47 {
			topic = "GPSLocation"
		} else if buffer[3] == 0x48 {
			topic = "HeartBeat"
		} else {
			fmt.Println("Unrecognized topic")
			continue
		}

		if strings.TrimSpace(string(buffer[0:n])) == "STOP" {
			fmt.Println("Exiting UDP Server")
			quit <- struct{}{} // quit
		}

		mqttsnMessage := buffer[7:n]
		macstring := hex.EncodeToString(mqttsnMessage[2 : 2+6])

		fmt.Println("Updating mac address and remote map : \n ")
		fmt.Println(macstring)
		fmt.Println(remoteAddr)

		update <- &m2a{macstring, remoteAddr}
		fmt.Println("Updated mac address and remote map : \n ")

		fmt.Println("Redirecting messages : \n ")
		token := client.Publish(topic, 0, false, mqttsnMessage)
		if token.Error() != nil {
			fmt.Println("CLIENT Error sending message")
		}
		fmt.Println("Messages redirected : \n ")

		fmt.Println(" ++++++++++ Receiving HeartBeat Ack : End ++++++++++ ")
	}
}

func main() {
	arguments := os.Args
	runtime.GOMAXPROCS(runtime.NumCPU())

	if len(arguments) == 1 {
		fmt.Println("Please provide a port number")
		return
	}
	PORT := ":" + arguments[1]

	s, err := net.ResolveUDPAddr("udp4", PORT)
	if err != nil {
		fmt.Println(err)
		return
	}

	connection, err := net.ListenUDP("udp4", s)
	if err != nil {
		fmt.Println(err)
		return
	}

	defer connection.Close()

	macStr2Addr := make(map[string]*net.UDPAddr)
	update := make(chan *m2a)

	go updateMacMap(macStr2Addr, update)

	quit := make(chan struct{})
	for i := 0; i < runtime.NumCPU(); i++ {
		go handleMqttSNPacket(connection, quit, &macStr2Addr, update)
	}

	<-quit
}
