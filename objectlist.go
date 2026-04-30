package bacnet

import (
	"fmt"

	"github.com/Nortech-ai/bacNetIP/btypes"
)

func (c *client) objectListLen(dev btypes.Device) (int, error) {
	rp := btypes.PropertyData{
		Object: btypes.Object{
			ID: dev.ID,
			Properties: []btypes.Property{
				{
					Type:       btypes.PropObjectList,
					ArrayIndex: 0,
				},
			},
		},
	}

	resp, err := c.ReadProperty(dev, rp)
	if err != nil {
		return 0, fmt.Errorf("reading property failed for device %d: %w", dev.ID.Instance, err)
	}
	return extractObjectListLength(dev.ID, resp.Object.Properties)
}

func (c *client) objectsRange(dev btypes.Device, start, end int) ([]btypes.Object, error) {
	rpm := btypes.MultiplePropertyData{
		Objects: []btypes.Object{
			{
				ID: dev.ID,
			},
		},
	}

	for i := start; i <= end; i++ {
		rpm.Objects[0].Properties = append(rpm.Objects[0].Properties, btypes.Property{
			Type:       btypes.PropObjectList,
			ArrayIndex: uint32(i),
		})
	}
	resp, err := c.ReadMultiProperty(dev, rpm)
	if err != nil {
		return nil, fmt.Errorf("unable to read multiple properties for device %d range %d-%d: %w", dev.ID.Instance, start, end, err)
	}
	if len(resp.Objects) == 0 {
		return nil, fmt.Errorf("no data was returned for device %d object-list range %d-%d", dev.ID.Instance, start, end)
	}
	return extractObjectIDsForRange(dev.ID, start, end, resp.Objects[0].Properties)
}

const readPropRequestSize = 20

func objectCopy(dest btypes.ObjectMap, src []btypes.Object) {
	for _, o := range src {
		if dest[o.ID.Type] == nil {
			dest[o.ID.Type] = make(map[btypes.ObjectInstance]btypes.Object)
		}
		dest[o.ID.Type][o.ID.Instance] = o
	}

}

func (c *client) objectList(dev *btypes.Device) error {
	dev.Objects = make(btypes.ObjectMap)

	l, err := c.objectListLen(*dev)
	if err != nil {
		return fmt.Errorf("unable to get list length: %v", err)
	}

	// Scan size is broken
	scanSize := int(dev.MaxApdu) / readPropRequestSize
	if scanSize < 1 {
		scanSize = 1
	}
	i := 0
	for i = 0; i < l/scanSize; i++ {
		start := i*scanSize + 1
		end := (i + 1) * scanSize

		objs, err := c.objectsRange(*dev, start, end)
		if err != nil {
			return fmt.Errorf("unable to retrieve objects between %d and %d: %v", start, end, err)
		}
		objectCopy(dev.Objects, objs)
	}
	start := i*scanSize + 1
	end := l
	if start <= end {
		objs, err := c.objectsRange(*dev, start, end)
		if err != nil {
			return fmt.Errorf("unable to retrieve objects between %d and %d: %v", start, end, err)
		}
		objectCopy(dev.Objects, objs)
	}
	return nil
}

func (c *client) objectInformation(dev *btypes.Device, objs []btypes.Object) error {
	// Often times the map will re-arrange the order it spits out,
	// so we need to keep track since the response will be in the
	// same order we issue the commands.
	var keys []btypes.ObjectID
	rpm := btypes.MultiplePropertyData{
		Objects: []btypes.Object{},
	}

	for _, o := range objs {
		if o.ID.Type > maxStandardBacnetType {
			continue
		}
		keys = append(keys, o.ID)
		rpm.Objects = append(rpm.Objects, btypes.Object{
			ID: o.ID,
			Properties: []btypes.Property{
				{
					Type:       btypes.PropObjectName,
					ArrayIndex: btypes.ArrayAll,
				},
				{
					Type:       btypes.PropObjectType,
					ArrayIndex: btypes.ArrayAll,
				},
			},
		})

	}
	resp, err := c.ReadMultiProperty(*dev, rpm)
	if err != nil {
		return fmt.Errorf("unable to read multiple property for device %d: %w", dev.ID.Instance, err)
	}
	for i, r := range resp.Objects {
		if i >= len(keys) {
			return fmt.Errorf("response object index %d exceeds requested objects (%d) for device %d", i, len(keys), dev.ID.Instance)
		}
		name, objectType, err := extractObjectMetadata(dev.ID, keys[i], r.Properties)
		if err != nil {
			return err
		}
		obj := dev.Objects[keys[i].Type][keys[i].Instance]
		obj.Name = name
		obj.ID.Type = objectType
		dev.Objects[keys[i].Type][keys[i].Instance] = obj
	}
	return nil
}

func (c *client) allObjectInformation(dev *btypes.Device) error {
	objs := dev.ObjectSlice()
	incrSize := 5

	var err error
	for i := 0; i < len(objs); i += incrSize {
		subset := objs[i:min(i+incrSize, len(objs))]
		err = c.objectInformation(dev, subset)
		if err != nil {
			return err
		}
	}

	return nil
}

// Objects retrieves all the objects within the given device and returns a
// device with these objects. Along with the list of objects, it will also
// gather additional information from the object such as the name and
// description of the objects. The device returned contains all the name and
// description fields for all objects
func (c *client) Objects(dev btypes.Device) (btypes.Device, error) {
	err := c.objectList(&dev)
	if err != nil {
		return dev, fmt.Errorf("unable to get object list: %v", err)
	}
	err = c.allObjectInformation(&dev)
	if err != nil {
		return dev, fmt.Errorf("unable to get object's information: %v", err)
	}
	return dev, nil
}

func extractObjectListLength(devID btypes.ObjectID, props []btypes.Property) (int, error) {
	if len(props) != 1 {
		return 0, fmt.Errorf("device %d read-property object-list length: expected 1 property got %d", devID.Instance, len(props))
	}
	p := props[0]
	if p.Type != btypes.PropObjectList || p.ArrayIndex != 0 {
		return 0, fmt.Errorf("device %d read-property object-list length: expected property %d array index 0 got property %d[%d]", devID.Instance, btypes.PropObjectList, p.Type, p.ArrayIndex)
	}
	data, ok := p.Data.(uint32)
	if !ok {
		return 0, fmt.Errorf("device %d property %d[%d]: expected uint32 list length got %T", devID.Instance, p.Type, p.ArrayIndex, p.Data)
	}
	return int(data), nil
}

func extractObjectIDsForRange(devID btypes.ObjectID, start, end int, props []btypes.Property) ([]btypes.Object, error) {
	if start > end {
		return nil, fmt.Errorf("invalid range start=%d end=%d for device %d", start, end, devID.Instance)
	}
	expectedCount := end - start + 1
	indexed := make(map[int]btypes.ObjectID, expectedCount)
	sequential := make([]btypes.ObjectID, 0, expectedCount)
	hasIndexed := false

	for _, p := range props {
		if p.Type != btypes.PropObjectList {
			continue
		}
		id, ok := p.Data.(btypes.ObjectID)
		if !ok {
			return nil, fmt.Errorf("device %d property %d[%d]: expected %T got %T", devID.Instance, p.Type, p.ArrayIndex, btypes.ObjectID{}, p.Data)
		}

		idx := int(p.ArrayIndex)
		if idx >= start && idx <= end {
			// Prefer explicit indexed responses when devices include ArrayIndex.
			hasIndexed = true
			if _, exists := indexed[idx]; exists {
				return nil, fmt.Errorf("device %d duplicate object-list index %d in range %d-%d", devID.Instance, idx, start, end)
			}
			indexed[idx] = id
			continue
		}
		sequential = append(sequential, id)
	}

	ids := make([]btypes.ObjectID, 0, expectedCount)
	if hasIndexed {
		// Rebuild in request order so callers see deterministic object ordering.
		for idx := start; idx <= end; idx++ {
			id, ok := indexed[idx]
			if !ok {
				return nil, fmt.Errorf("device %d missing object-list index %d in range %d-%d", devID.Instance, idx, start, end)
			}
			ids = append(ids, id)
		}
	} else {
		// Some devices/gateways omit index metadata; accept strict sequential payloads.
		if len(sequential) != expectedCount {
			return nil, fmt.Errorf("device %d object-list range %d-%d expected %d entries got %d", devID.Instance, start, end, expectedCount, len(sequential))
		}
		ids = append(ids, sequential...)
	}

	objs := make([]btypes.Object, len(ids))
	for i, id := range ids {
		objs[i].ID = id
	}
	return objs, nil
}

func extractObjectMetadata(devID, requestedID btypes.ObjectID, props []btypes.Property) (string, btypes.ObjectType, error) {
	var (
		name      string
		objectTyp btypes.ObjectType
		nameOK    bool
		typeOK    bool
	)

	for _, p := range props {
		switch p.Type {
		case btypes.PropObjectName:
			v, ok := p.Data.(string)
			if !ok {
				return "", 0, fmt.Errorf("device %d object %s property %d: expected string got %T", devID.Instance, requestedID.String(), p.Type, p.Data)
			}
			name = v
			nameOK = true
		case btypes.PropObjectType:
			switch v := p.Data.(type) {
			case uint32:
				// Many devices encode object type as uint32 even though model uses ObjectType.
				objectTyp = btypes.ObjectType(v)
				typeOK = true
			case btypes.ObjectType:
				objectTyp = v
				typeOK = true
			default:
				return "", 0, fmt.Errorf("device %d object %s property %d: expected uint32 or ObjectType got %T", devID.Instance, requestedID.String(), p.Type, p.Data)
			}
		}
	}

	if !nameOK || !typeOK {
		// Require both fields before mutating stored object metadata.
		return "", 0, fmt.Errorf("device %d object %s missing required metadata (name=%t objectType=%t)", devID.Instance, requestedID.String(), nameOK, typeOK)
	}
	return name, objectTyp, nil
}
