package diameter

import (
	"errors"
	"net"
	"os"
	"time"

	"github.com/fiorix/go-diameter/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
	"github.com/fiorix/go-diameter/v4/diam/sm"
	log "github.com/sirupsen/logrus"
)

type DiameterClient struct {
	client         *sm.Client
	conn           diam.Conn
	hopIds         map[uint32]chan *diam.Message
	requestTimeout time.Duration
}

type DiameterMessage struct {
	diamMsg *diam.Message
}

type DataType struct{}

type AVP struct{}

type Dict struct{}

func (*Diameter) XClient(arg map[string]interface{}) (*DiameterClient, error) {

	config, err := parseConfig(arg)
	if err != nil {
		return nil, err
	}

	hostIPAddresses := []datatype.Address{}
	for _, ip := range *config.CapabilityExchange.HostIPAddresses {
		hostIPAddresses = append(hostIPAddresses, datatype.Address(net.ParseIP(ip)))
	}

	cfg := &sm.Settings{
		OriginHost:       datatype.DiameterIdentity(*config.CapabilityExchange.OriginHost),
		OriginRealm:      datatype.DiameterIdentity(*config.CapabilityExchange.OriginRealm),
		VendorID:         datatype.Unsigned32(*config.CapabilityExchange.VendorID),
		ProductName:      datatype.UTF8String(*config.CapabilityExchange.ProductName),
		OriginStateID:    datatype.Unsigned32(time.Now().Unix()),
		FirmwareRevision: datatype.Unsigned32(*config.CapabilityExchange.FirmwareRevision),
		HostIPAddresses:  hostIPAddresses,
	}
	mux := sm.New(cfg)

	hopIds := make(map[uint32]chan *diam.Message)
	mux.Handle("ALL", handleResponse(hopIds))

	client := &sm.Client{
		Dict:               dict.Default,
		Handler:            mux,
		MaxRetransmits:     *config.MaxRetransmits,
		RetransmitInterval: *&config.RetransmitInterval.Duration,
		EnableWatchdog:     *config.EnableWatchdog,
		WatchdogInterval:   *&config.WatchdogInterval.Duration,
		WatchdogStream:     *config.WatchdogStream,
		AuthApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(4)), // TODO make configurable
		},
	}

	return &DiameterClient{
		client:         client,
		conn:           nil,
		hopIds:         hopIds,
		requestTimeout: config.RequestTimeout.Duration,
	}, nil
}

func handleResponse(hopIds map[uint32]chan *diam.Message) diam.HandlerFunc {
	return func(_ diam.Conn, m *diam.Message) {
		hopByHopID := m.Header.HopByHopID
		v, exists := hopIds[hopByHopID]
		if !exists {
			log.Errorf("Received unexpected response with Hop-by-Hop ID %d\n", hopByHopID)
		} else {
			v <- m
		}
	}
}

func (c *DiameterClient) Connect(address string) error {
	if c.conn != nil {
		return nil
	}

	conn, err := c.client.DialNetwork("tcp", address)
	if err != nil {
		log.Errorf("Error connecting to %s, %v\n", "localhost:3868", err)
		return err
	}
	log.Infof("Connected to %s\n", "localhost:3868")

	c.conn = conn
	return nil
}

func (c *DiameterClient) Send(msg *DiameterMessage) (uint32, error) {

	if c.conn == nil {
		return 0, errors.New("Not connected")
	}

	req := msg.diamMsg

	// Keep track of Hop-by-Hop ID
	hopByHopID := req.Header.HopByHopID
	c.hopIds[hopByHopID] = make(chan *diam.Message)

	// Timeout settings
	timeout := time.After(c.requestTimeout)

	// Send Request
	_, err := req.WriteTo(c.conn)
	if err != nil {
		return uint32(0), err
	}

	// Wait for Response
	var resp *diam.Message
	select {
	case resp = <-c.hopIds[hopByHopID]:
	case <-timeout:
		return uint32(5012), errors.New("Response timeout")
	}

	delete(c.hopIds, hopByHopID)

	resultCodeAvp, err := resp.FindAVP(avp.ResultCode, 0)
	if err != nil {
		return uint32(0), errors.New("Result-Code AVP not found")
	}
	resultCode := resultCodeAvp.Data.(datatype.Unsigned32)

	return uint32(resultCode), nil
}

func (*Diameter) NewMessage(cmd uint32, appid uint32) *DiameterMessage {
	return &DiameterMessage{
		diamMsg: diam.NewRequest(cmd, appid, dict.Default),
	}
}

func (m *DiameterMessage) XAVP(code uint32, vendor uint32, flags uint8, data datatype.Type) {
	m.diamMsg.NewAVP(code, flags, vendor, data)
}

func (m *DiameterMessage) Print() string {
	return m.diamMsg.PrettyDump()
}

func (*Diameter) XDataType() DataType {
	return DataType{}
}

func (d *DataType) XAddress(value string) datatype.Type {
	return datatype.Address(value)
}

func (d *DataType) XDiameterIdentity(value string) datatype.Type {
	return datatype.DiameterIdentity(value)
}

func (d *DataType) XDiameterURI(value string) datatype.Type {
	return datatype.DiameterURI(value)
}

func (d *DataType) XEnumerated(value int32) datatype.Type {
	return datatype.Enumerated(value)
}

func (d *DataType) XFloat32(value float32) datatype.Type {
	return datatype.Float32(value)
}

func (d *DataType) XFloat64(value float64) datatype.Type {
	return datatype.Float64(value)
}

func (d *DataType) XGrouped(avps []*diam.AVP) datatype.Type {
	return &diam.GroupedAVP{
		AVP: avps,
	}
}

func (d *DataType) XIPFilterRule(value string) datatype.Type {
	return datatype.IPFilterRule(value)
}

func (d *DataType) XIPv4(value string) datatype.Type {
	return datatype.IPv4(value)
}

func (d *DataType) XIPv6(value string) datatype.Type {
	return datatype.IPv6(value)
}

func (d *DataType) XInteger32(value int32) datatype.Type {
	return datatype.Integer32(value)
}

func (d *DataType) XInteger64(value int64) datatype.Type {
	return datatype.Integer64(value)
}

func (d *DataType) XOctetString(value string) datatype.Type {
	return datatype.OctetString(value)
}

func (d *DataType) XQoSFilterRule(value string) datatype.Type {
	return datatype.QoSFilterRule(value)
}

func (d *DataType) XTime(value time.Time) datatype.Type {
	return datatype.Time(value)
}

func (d *DataType) XUTF8String(value string) datatype.Type {
	return datatype.UTF8String(value)
}

func (d *DataType) XUnsigned32(value uint32) datatype.Type {
	return datatype.Unsigned32(value)
}

func (d *DataType) XUnsigned64(value uint64) datatype.Type {
	return datatype.Unsigned64(value)
}

func (a *AVP) XNew(code uint32, vendor uint32, flags uint8, data datatype.Type) *diam.AVP {
	return diam.NewAVP(code, flags, vendor, data)
}

func (*Dict) Load(dictionary string) error {
	file, err := os.Open(dictionary)
	if err != nil {
		return err
	}
	defer file.Close()

	dict.Default.Load(file)
	if err != nil {
		return err
	}
	return nil
}
