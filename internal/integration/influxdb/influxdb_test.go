package influxdb

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/brocaar/chirpstack-application-server/internal/codec"
	"github.com/brocaar/chirpstack-application-server/internal/integration"
	"github.com/brocaar/lorawan"
)

func init() {
	log.SetLevel(log.ErrorLevel)
}

type testHTTPHandler struct {
	requests chan *http.Request
}

func (h *testHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := ioutil.ReadAll(r.Body)
	r.Body = ioutil.NopCloser(bytes.NewReader(b))
	h.requests <- r
	w.WriteHeader(http.StatusOK)
}

type HandlerTestSuite struct {
	suite.Suite

	Handler  integration.Integrator
	Requests chan *http.Request
	Server   *httptest.Server
}

func (ts *HandlerTestSuite) SetupSuite() {
	assert := require.New(ts.T())
	ts.Requests = make(chan *http.Request, 100)

	httpHandler := testHTTPHandler{
		requests: ts.Requests,
	}
	ts.Server = httptest.NewServer(&httpHandler)

	conf := Config{
		Endpoint:            ts.Server.URL + "/write",
		DB:                  "chirpstack",
		Username:            "user",
		Password:            "password",
		RetentionPolicyName: "DEFAULT",
		Precision:           "s",
	}
	var err error
	ts.Handler, err = New(conf)
	assert.NoError(err)
}

func (ts *HandlerTestSuite) TearDownSuite() {
	ts.Server.Close()
}

func (ts *HandlerTestSuite) TestStatus() {
	tests := []struct {
		Name         string
		Payload      integration.StatusNotification
		ExpectedBody string
	}{
		{
			Name: "margin and battery status",
			Payload: integration.StatusNotification{
				ApplicationName: "test-app",
				DevEUI:          lorawan.EUI64{1, 2, 3, 4, 5, 6, 7, 8},
				DeviceName:      "test-device",
				Battery:         123,
				BatteryLevel:    48.43,
				Margin:          10,
				Tags: map[string]string{
					"foo": "bar",
				},
			},
			ExpectedBody: `device_status_battery,application_name=test-app,dev_eui=0102030405060708,device_name=test-device,foo=bar value=123i
device_status_battery_level,application_name=test-app,dev_eui=0102030405060708,device_name=test-device,foo=bar value=48.430000
device_status_margin,application_name=test-app,dev_eui=0102030405060708,device_name=test-device,foo=bar value=10i`,
		},
	}

	for _, tst := range tests {
		ts.T().Run(tst.Name, func(t *testing.T) {
			assert := require.New(t)
			assert.NoError(ts.Handler.SendStatusNotification(context.Background(), tst.Payload))
			req := <-ts.Requests
			assert.Equal("/write", req.URL.Path)
			assert.Equal(url.Values{
				"db":        []string{"chirpstack"},
				"precision": []string{"s"},
				"rp":        []string{"DEFAULT"},
			}, req.URL.Query())

			b, err := ioutil.ReadAll(req.Body)
			assert.NoError(err)
			assert.Equal(tst.ExpectedBody, string(b))

			user, pw, ok := req.BasicAuth()
			assert.Equal("user", user)
			assert.Equal("password", pw)
			assert.True(ok)

			assert.Equal("text/plain", req.Header.Get("Content-Type"))
		})
	}
}

func (ts *HandlerTestSuite) TestUplink() {
	tests := []struct {
		Name         string
		Payload      integration.DataUpPayload
		ExpectedBody string
	}{
		{
			Name: "One level depth",
			Payload: integration.DataUpPayload{
				ApplicationName: "test-app",
				DeviceName:      "test-dev",
				DevEUI:          lorawan.EUI64{1, 2, 3, 4, 5, 6, 7, 8},
				FCnt:            10,
				FPort:           20,
				TXInfo: integration.TXInfo{
					Frequency: 868100000,
					DR:        2,
				},
				Object: map[string]interface{}{
					"temperature": 25.4,
					"humidity":    20,
					"active":      true,
					"status":      "on",
				},
				Tags: map[string]string{
					"fo o": "ba,r",
				},
			},
			ExpectedBody: `device_frmpayload_data_active,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,fo\ o=ba\,r value=true
device_frmpayload_data_humidity,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,fo\ o=ba\,r value=20i
device_frmpayload_data_status,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,fo\ o=ba\,r value="on"
device_frmpayload_data_temperature,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,fo\ o=ba\,r value=25.400000
device_uplink,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,dr=2,fo\ o=ba\,r,frequency=868100000 f_cnt=10i,value=1i`,
		},
		{
			Name: "One level depth with nil value",
			Payload: integration.DataUpPayload{
				ApplicationName: "test-app",
				DeviceName:      "test-dev",
				DevEUI:          lorawan.EUI64{1, 2, 3, 4, 5, 6, 7, 8},
				FCnt:            10,
				FPort:           20,
				TXInfo: integration.TXInfo{
					Frequency: 868100000,
					DR:        2,
				},
				Object: map[string]interface{}{
					"temperature": nil,
					"humidity":    20,
					"active":      true,
					"status":      "on",
				},
				Tags: map[string]string{
					"fo=o": "bar",
				},
			},
			ExpectedBody: `device_frmpayload_data_active,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,fo\=o=bar value=true
device_frmpayload_data_humidity,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,fo\=o=bar value=20i
device_frmpayload_data_status,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,fo\=o=bar value="on"
device_uplink,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,dr=2,fo\=o=bar,frequency=868100000 f_cnt=10i,value=1i`,
		},
		{
			Name: "One level depth + RXInfo",
			Payload: integration.DataUpPayload{
				ApplicationName: "test-app",
				DeviceName:      "test-dev",
				DevEUI:          lorawan.EUI64{1, 2, 3, 4, 5, 6, 7, 8},
				FCnt:            10,
				FPort:           20,
				TXInfo: integration.TXInfo{
					Frequency: 868100000,
					DR:        2,
				},
				RXInfo: []integration.RXInfo{
					{
						LoRaSNR: 1,
						RSSI:    -60,
					},
					{
						LoRaSNR: 2.5,
						RSSI:    -55,
					},
					{
						LoRaSNR: 1,
						RSSI:    -70,
					},
				},
				Object: map[string]interface{}{
					"temperature": 25.4,
					"humidity":    20,
					"active":      true,
					"status":      "on",
				},
				Tags: map[string]string{
					"foo": "bar",
				},
			},
			ExpectedBody: `device_frmpayload_data_active,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,foo=bar value=true
device_frmpayload_data_humidity,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,foo=bar value=20i
device_frmpayload_data_status,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,foo=bar value="on"
device_frmpayload_data_temperature,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,foo=bar value=25.400000
device_uplink,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,dr=2,foo=bar,frequency=868100000 f_cnt=10i,rssi=-55i,snr=2.500000,value=1i`,
		},
		{
			Name: "Mixed level depth",
			Payload: integration.DataUpPayload{
				ApplicationName: "test-app",
				DeviceName:      "test-dev",
				DevEUI:          lorawan.EUI64{1, 2, 3, 4, 5, 6, 7, 8},
				FCnt:            10,
				FPort:           20,
				TXInfo: integration.TXInfo{
					Frequency: 868100000,
					DR:        2,
				},
				Object: map[string]interface{}{
					"temperature": map[string]interface{}{
						"a": 20.5,
						"b": 33.3,
					},
					"humidity": 20,
					"active":   true,
					"status":   "on",
				},
				Tags: map[string]string{
					"foo": "bar",
				},
			},
			ExpectedBody: `device_frmpayload_data_active,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,foo=bar value=true
device_frmpayload_data_humidity,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,foo=bar value=20i
device_frmpayload_data_status,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,foo=bar value="on"
device_frmpayload_data_temperature_a,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,foo=bar value=20.500000
device_frmpayload_data_temperature_b,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,foo=bar value=33.300000
device_uplink,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,dr=2,foo=bar,frequency=868100000 f_cnt=10i,value=1i`,
		},
		{
			Name: "One level depth + device status fields",
			Payload: integration.DataUpPayload{
				ApplicationName: "test-app",
				DeviceName:      "test-dev",
				DevEUI:          lorawan.EUI64{1, 2, 3, 4, 5, 6, 7, 8},
				FCnt:            10,
				FPort:           20,
				TXInfo: integration.TXInfo{
					Frequency: 868100000,
					DR:        2,
				},
				Object: map[string]interface{}{
					"temperature": 25.4,
					"humidity":    20,
					"active":      true,
					"status":      "on",
				},
				Tags: map[string]string{
					"foo": "bar",
				},
			},
			ExpectedBody: `device_frmpayload_data_active,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,foo=bar value=true
device_frmpayload_data_humidity,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,foo=bar value=20i
device_frmpayload_data_status,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,foo=bar value="on"
device_frmpayload_data_temperature,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,foo=bar value=25.400000
device_uplink,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,dr=2,foo=bar,frequency=868100000 f_cnt=10i,value=1i`,
		},
		{
			Name: "Latitude and longitude",
			Payload: integration.DataUpPayload{
				ApplicationName: "test-app",
				DeviceName:      "test-dev",
				DevEUI:          lorawan.EUI64{1, 2, 3, 4, 5, 6, 7, 8},
				FCnt:            10,
				FPort:           20,
				TXInfo: integration.TXInfo{
					Frequency: 868100000,
					DR:        2,
				},
				Object: map[string]interface{}{
					"latitude":  1.123,
					"longitude": 2.123,
					"active":    true,
					"status":    "on",
				},
				Tags: map[string]string{
					"foo": "bar",
				},
			},
			ExpectedBody: `device_frmpayload_data_active,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,foo=bar value=true
device_frmpayload_data_location,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,foo=bar geohash="s01w2k3vvqre",latitude=1.123000,longitude=2.123000
device_frmpayload_data_status,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,foo=bar value="on"
device_uplink,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,dr=2,foo=bar,frequency=868100000 f_cnt=10i,value=1i`,
		},
		{
			Name: "Cayenne LPP with latitude and longitude",
			Payload: integration.DataUpPayload{
				ApplicationName: "test-app",
				DeviceName:      "test-dev",
				DevEUI:          lorawan.EUI64{1, 2, 3, 4, 5, 6, 7, 8},
				FCnt:            10,
				FPort:           20,
				TXInfo: integration.TXInfo{
					Frequency: 868100000,
					DR:        2,
				},
				Object: &codec.CayenneLPP{
					GPSLocation: map[byte]codec.GPSLocation{
						10: codec.GPSLocation{
							Latitude:  1.123,
							Longitude: 2.123,
							Altitude:  3.123,
						},
					},
				},
				Tags: map[string]string{
					"foo": "bar",
				},
			},
			ExpectedBody: `device_frmpayload_data_gps_location_10_altitude,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,foo=bar value=3.123000
device_frmpayload_data_gps_location_10_location,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,f_port=20,foo=bar geohash="s01w2k3vvqre",latitude=1.123000,longitude=2.123000
device_uplink,application_name=test-app,dev_eui=0102030405060708,device_name=test-dev,dr=2,foo=bar,frequency=868100000 f_cnt=10i,value=1i`,
		},
	}

	for _, tst := range tests {
		ts.T().Run(tst.Name, func(t *testing.T) {
			assert := require.New(t)
			assert.NoError(ts.Handler.SendDataUp(context.Background(), tst.Payload))
			req := <-ts.Requests
			assert.Equal("/write", req.URL.Path)
			assert.Equal(url.Values{
				"db":        []string{"chirpstack"},
				"precision": []string{"s"},
				"rp":        []string{"DEFAULT"},
			}, req.URL.Query())

			b, err := ioutil.ReadAll(req.Body)
			assert.NoError(err)
			assert.Equal(tst.ExpectedBody, string(b))

			user, pw, ok := req.BasicAuth()
			assert.Equal("user", user)
			assert.Equal("password", pw)
			assert.True(ok)

			assert.Equal("text/plain", req.Header.Get("Content-Type"))
		})
	}
}

func TestHandler(t *testing.T) {
	suite.Run(t, new(HandlerTestSuite))
}
