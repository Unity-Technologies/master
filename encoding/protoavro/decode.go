package protoavro

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// decodeJSON decodes the JSON encoded avro data and places the
// result in msg.
func decodeJSON(data interface{}, msg proto.Message, options *UnmarshalOptions) error {
	return decodeMessage(data, msg.ProtoReflect(), options)
}

func decodeMessage(data interface{}, msg protoreflect.Message, options *UnmarshalOptions) error {
	if data == nil {
		return nil
	}
	d, ok := data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("expected message encoded as map[string]interface{}, got %T", data)
	}

	if isWKT(msg.Descriptor().FullName()) {
		return decodeWKT(d, msg)
	}
	// unwrap union
	desc := msg.Descriptor()
	if msgData, ok := d[string(desc.FullName())]; len(d) == 1 && ok {
		return decodeMessage(msgData, msg, options)
	}
	for fieldName, fieldValue := range d {
		fd, ok := findField(desc, fieldName, options)
		if !ok {
			return fmt.Errorf("unexpected field %s", fieldName)
		}
		if fd == nil {
			continue
		}
		if err := decodeField(fieldValue, msg, fd, options); err != nil {
			return err
		}
	}
	return nil
}

func decodeField(data interface{}, val protoreflect.Message, f protoreflect.FieldDescriptor, options *UnmarshalOptions) error {
	if data == nil {
		return nil
	}
	switch {
	case f.IsMap():
		mp := val.NewField(f).Map()
		if err := decodeMap(data, f, mp, options); err != nil {
			return err
		}
		val.Set(f, protoreflect.ValueOfMap(mp))
		return nil
	case f.IsList():
		listData, err := decodeListLike(data, "array")
		if err != nil {
			return err
		}
		list := val.NewField(f).List()
		for _, el := range listData {
			if el == nil {
				list.Append(list.NewElement())
				continue
			}
			fieldValue, err := decodeFieldKind(el, list.NewElement(), f, options)
			if err != nil {
				return err
			}
			list.Append(fieldValue)
		}
		val.Set(f, protoreflect.ValueOfList(list))
		return nil
	default:
		fieldValue, err := decodeFieldKind(data, val.NewField(f), f, options)
		if err != nil {
			return err
		}
		val.Set(f, fieldValue)
	}
	return nil
}

func decodeFieldKind(data interface{}, mutable protoreflect.Value, f protoreflect.FieldDescriptor, options *UnmarshalOptions) (protoreflect.Value, error) {
	switch f.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if err := decodeMessage(data, mutable.Message(), options); err != nil {
			return protoreflect.Value{}, err
		}
		return mutable, nil
	case protoreflect.StringKind:
		str, err := decodeStringLike(data, "string")
		if err != nil {
			return protoreflect.Value{}, fmt.Errorf("field %s: %w", f.Name(), err)
		}
		return protoreflect.ValueOfString(str), nil
	case protoreflect.BoolKind:
		bo, err := decodeBoolLike(data, "boolean")
		if err != nil {
			return protoreflect.Value{}, fmt.Errorf("field %s: %w", f.Name(), err)
		}
		return protoreflect.ValueOfBool(bo), nil
	case protoreflect.Int32Kind, protoreflect.Sfixed32Kind, protoreflect.Sint32Kind:
		i, err := decodeIntLike(data, "int")
		if err != nil {
			return protoreflect.Value{}, fmt.Errorf("field %s: %w", f.Name(), err)
		}
		return protoreflect.ValueOfInt32(int32(i)), nil
	case protoreflect.Int64Kind, protoreflect.Sfixed64Kind, protoreflect.Sint64Kind:
		i, err := decodeIntLike(data, "long")
		if err != nil {
			return protoreflect.Value{}, fmt.Errorf("field %s: %w", f.Name(), err)
		}
		return protoreflect.ValueOfInt64(i), nil
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		i, err := decodeIntLike(data, "int")
		if err != nil {
			return protoreflect.Value{}, fmt.Errorf("field %s: %w", f.Name(), err)
		}
		return protoreflect.ValueOfUint32(uint32(i)), nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		i, err := decodeIntLike(data, "long")
		if err != nil {
			return protoreflect.Value{}, fmt.Errorf("field %s: %w", f.Name(), err)
		}
		return protoreflect.ValueOfUint64(uint64(i)), nil
	case protoreflect.BytesKind:
		bs, err := decodeBytesLike(data, "bytes")
		if err != nil {
			return protoreflect.Value{}, fmt.Errorf("field %s: %w", f.Name(), err)
		}
		return protoreflect.ValueOfBytes(bs), nil
	case protoreflect.EnumKind:
		str, err := decodeStringLike(data, string(f.Enum().FullName()))
		if err != nil {
			return protoreflect.Value{}, fmt.Errorf("field %s: %w", f.Name(), err)
		}
		if v := f.Enum().Values().ByName(protoreflect.Name(str)); v != nil {
			return protoreflect.ValueOfEnum(v.Number()), nil
		} else {
			return protoreflect.ValueOfEnum(0), nil
		}
	case protoreflect.DoubleKind:
		dbl, ok := data.(float64)
		if !ok {
			return protoreflect.Value{}, fmt.Errorf("field %s: expected float64, got %T", f.Name(), data)
		}
		return protoreflect.ValueOfFloat64(dbl), nil
	case protoreflect.FloatKind:
		flt, ok := data.(float32)
		if !ok {
			return protoreflect.Value{}, fmt.Errorf("field %s: expected float32, got %T", f.Name(), data)
		}
		return protoreflect.ValueOfFloat32(flt), nil

	}
	return protoreflect.Value{}, fmt.Errorf("unexpected kind %s", f.Kind())
}

func findField(desc protoreflect.MessageDescriptor, name string, options *UnmarshalOptions) (protoreflect.FieldDescriptor, bool) {
	if fd := desc.Fields().ByJSONName(name); fd != nil {
		return fd, true
	}
	if fd := desc.Fields().ByTextName(name); fd != nil {
		return fd, true
	}
	for _, extraField := range options.MarshalOptions.ExtraFields {
		if extraField.FieldName == name {
			return nil,true
		}
	}
	return nil, false
}
