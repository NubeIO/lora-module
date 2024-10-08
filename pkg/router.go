package pkg

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/NubeIO/lib-module-go/nhttp"
	"github.com/NubeIO/lib-module-go/nmodule"
	"github.com/NubeIO/lib-module-go/router"
	"github.com/NubeIO/lib-utils-go/nstring"
	"github.com/NubeIO/module-core-loraraw/aesutils"
	"github.com/NubeIO/module-core-loraraw/endec"
	"github.com/NubeIO/module-core-loraraw/schema"
	"github.com/NubeIO/nubeio-rubix-lib-models-go/dto"
	"github.com/NubeIO/nubeio-rubix-lib-models-go/model"
	"github.com/NubeIO/nubeio-rubix-lib-models-go/nargs"
	log "github.com/sirupsen/logrus"
)

var route *router.Router

func InitRouter() {
	route = router.NewRouter()

	route.Handle(nhttp.GET, "/api/networks/schema", GetNetworkSchema)
	route.Handle(nhttp.GET, "/api/devices/schema", GetDeviceSchema)
	route.Handle(nhttp.GET, "/api/points/schema", GetPointSchema)

	route.Handle(nhttp.POST, "/api/networks", CreateNetwork)
	route.Handle(nhttp.PATCH, "/api/networks/:uuid", UpdateNetwork)
	route.Handle(nhttp.DELETE, "/api/networks/:uuid", DeleteNetwork)

	route.Handle(nhttp.POST, "/api/devices", CreateDevice)
	route.Handle(nhttp.PATCH, "/api/devices/:uuid", UpdateDevice)
	route.Handle(nhttp.DELETE, "/api/devices/:uuid", DeleteDevice)

	route.Handle(nhttp.POST, "/api/points", CreatePoint)
	route.Handle(nhttp.PATCH, "/api/points/:uuid", UpdatePoint)
	route.Handle(nhttp.PATCH, "/api/points/:uuid/write", PointWrite)
	route.Handle(nhttp.DELETE, "/api/points/:uuid", DeletePoint)
}

func (m *Module) CallModule(method nhttp.Method, urlString string, headers http.Header, body []byte) ([]byte, error) {
	mo := (nmodule.Module)(m)
	return route.CallHandler(&mo, method, urlString, headers, body)
}

func GetNetworkSchema(m *nmodule.Module, r *router.Request) ([]byte, error) {
	return json.Marshal(schema.GetNetworkSchema())
}

func GetDeviceSchema(m *nmodule.Module, r *router.Request) ([]byte, error) {
	return json.Marshal(schema.GetDeviceSchema())
}

func GetPointSchema(m *nmodule.Module, r *router.Request) ([]byte, error) {
	return json.Marshal(schema.GetPointSchema())
}

func CreateNetwork(m *nmodule.Module, r *router.Request) ([]byte, error) {
	var network *model.Network
	err := json.Unmarshal(r.Body, &network)
	if err != nil {
		return nil, err
	}
	net, err := (*m).(*Module).addNetwork(network)
	if err != nil {
		return nil, err
	}
	return json.Marshal(net)
}

func UpdateNetwork(m *nmodule.Module, r *router.Request) ([]byte, error) {
	var network *model.Network
	err := json.Unmarshal(r.Body, &network)
	if err != nil {
		return nil, err
	}
	net, err := (*m).(*Module).grpcMarshaller.UpdateNetwork(r.PathParams["uuid"], network)
	if err != nil {
		return nil, err
	}
	return json.Marshal(net)
}

func DeleteNetwork(m *nmodule.Module, r *router.Request) ([]byte, error) {
	err := (*m).(*Module).grpcMarshaller.DeleteNetwork(r.PathParams["uuid"])
	return nil, err
}

func CreateDevice(m *nmodule.Module, r *router.Request) ([]byte, error) {
	var device *model.Device
	err := json.Unmarshal(r.Body, &device)
	if err != nil {
		return nil, err
	}
	v := r.QueryParams.Get(nargs.WithPoints)
	dev, err := (*m).(*Module).addDevice(device, v == "true")
	if err != nil {
		return nil, err
	}
	return json.Marshal(dev)
}

func UpdateDevice(m *nmodule.Module, r *router.Request) ([]byte, error) {
	var device *model.Device
	err := json.Unmarshal(r.Body, &device)
	if err != nil {
		return nil, err
	}
	dev, err := (*m).(*Module).grpcMarshaller.UpdateDevice(r.PathParams["uuid"], device)
	if err != nil {
		return nil, err
	}

	_ = (*m).(*Module).updateDevicePointsAddress(dev)

	return json.Marshal(dev)
}

func DeleteDevice(m *nmodule.Module, r *router.Request) ([]byte, error) {
	err := (*m).(*Module).grpcMarshaller.DeleteDevice(r.PathParams["uuid"])
	return nil, err
}

func CreatePoint(m *nmodule.Module, r *router.Request) ([]byte, error) {
	var point *model.Point
	err := json.Unmarshal(r.Body, &point)
	if err != nil {
		return nil, err
	}
	pnt, err := (*m).(*Module).addPoint(point)
	if err != nil {
		return nil, err
	}
	return json.Marshal(pnt)
}

func UpdatePoint(m *nmodule.Module, r *router.Request) ([]byte, error) {
	var point *model.Point
	err := json.Unmarshal(r.Body, &point)
	if err != nil {
		return nil, err
	}
	pnt, err := (*m).(*Module).updatePoint(r.PathParams["uuid"], point)
	if err != nil {
		return nil, err
	}
	return json.Marshal(pnt)
}

func SafeDereferenceUint8(ptr *int) (uint8, error) {
	if ptr == nil {
		return 0, errors.New("attempting to dereference a nil uint8 pointer")
	}
	return uint8(*ptr), nil
}

func PointWrite(m *nmodule.Module, r *router.Request) ([]byte, error) {
	var pw *dto.PointWriter
	err := json.Unmarshal(r.Body, &pw)
	if err != nil {
		return nil, err
	}

	pnt, err := (*m).(*Module).grpcMarshaller.GetPoint(r.PathParams["uuid"])
	if err != nil {
		return nil, err
	}

	serialData := endec.NewSerialData()
	endec.SetPositionalData(serialData, true)
	endec.SetRequestData(serialData, true)
	msgId, _ := endec.GenerateRandomId()
	endec.SetMessageId(serialData, msgId)
	endec.UpdateBitPositionsForHeaderByte(serialData)

	log.Infof("Point Address UUID >>>>>>> %s ", nstring.DerefString(pnt.AddressUUID))

	// If the PointWriter struct has fields, you can also print them individually
	// Assuming pw has fields like `Name` and `Value`
	for _, value := range *pw.Priority {
		if value != nil {
			floatValue := *value
			pointDataType, err := strconv.Atoi(pnt.DataType) // As DataType is store as string
			if err != nil {
				return nil, err
			}
			addressID, err := SafeDereferenceUint8(pnt.AddressID)
			if err != nil {
				return nil, err
			}
			if endec.MetaDataKey(pointDataType) == endec.MDK_UINT_8 {
				endec.EncodeData(serialData, uint8(floatValue), endec.MetaDataKey(pointDataType), addressID)
			} else if endec.MetaDataKey(pointDataType) == endec.MDK_UINT_16 {
				endec.EncodeData(serialData, uint16(floatValue), endec.MetaDataKey(pointDataType), addressID)
			} else if endec.MetaDataKey(pointDataType) == endec.MDK_UINT_32 {
				endec.EncodeData(serialData, uint32(floatValue), endec.MetaDataKey(pointDataType), addressID)
			} else if endec.MetaDataKey(pointDataType) == endec.MDK_UINT_64 {
				endec.EncodeData(serialData, uint64(floatValue), endec.MetaDataKey(pointDataType), addressID)
			} else if endec.MetaDataKey(pointDataType) == endec.MDK_INT_8 {
				endec.EncodeData(serialData, int8(floatValue), endec.MetaDataKey(pointDataType), addressID)
			} else if endec.MetaDataKey(pointDataType) == endec.MDK_INT_16 {
				endec.EncodeData(serialData, int16(floatValue), endec.MetaDataKey(pointDataType), addressID)
			} else if endec.MetaDataKey(pointDataType) == endec.MDK_INT_32 {
				endec.EncodeData(serialData, int32(floatValue), endec.MetaDataKey(pointDataType), addressID)
			} else if endec.MetaDataKey(pointDataType) == endec.MDK_INT_64 {
				endec.EncodeData(serialData, int64(floatValue), endec.MetaDataKey(pointDataType), addressID)
			} else {
				endec.EncodeData(serialData, floatValue, endec.MetaDataKey(pointDataType), addressID)
			}
		}
	}

	device, err := (*m).(*Module).grpcMarshaller.GetDevice(pnt.DeviceUUID)
	if err != nil {
		return nil, err
	}

	hexKey := (*m).(*Module).config.DefaultKey
	if device.Manufacture != "" {
		hexKey = device.Manufacture // Manufacture property from device model holds hex key
	}

	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, err
	}

	// Encrypt the encoded data by using aesutils.Encrypt method
	encryptedData, err := aesutils.Encrypt(
		nstring.DerefString(pnt.AddressUUID),
		serialData.Buffer,
		key,
		0,
	)

	pendingPointWrite := &PendingPointWrite{MessageId: msgId, Message: encryptedData}
	(*m).(*Module).pointWriteQueue.EnqueueWriteQueue(pendingPointWrite)

	pointWriteResponse, err := (*m).(*Module).grpcMarshaller.PointWrite(r.PathParams["uuid"], pw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(pointWriteResponse.Point)
}

func DeletePoint(m *nmodule.Module, r *router.Request) ([]byte, error) {
	err := (*m).(*Module).grpcMarshaller.DeletePoint(r.PathParams["uuid"])
	return nil, err
}
