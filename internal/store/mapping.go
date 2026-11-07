package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"dbsrv/internal/auth"
	"dbsrv/internal/wire"
)

// Item containers (items.container).
const (
	ContainerEquip     = 0
	ContainerInventory = 1
	ContainerStorage   = 2
	ContainerFilter    = 3
	ContainerSubEquip  = 4

	storageCharSlot = -1
)

// AccountRecord is an account as DBSRV works with it: the C++ STRUCT_ACCOUNT
// image (secrets removed) plus the columns that replace them.
type AccountRecord struct {
	ID              int64
	Status          auth.AccountStatus
	PasswordHash    string
	NumericPassHash string // "" = not set yet (first 0xFDE request sets it)
	Account         wire.Account
}

type accountRow struct {
	Username        string
	PasswordHash    string
	Status          int16
	NumericPassHash *string
	PassBlock       string
	AccessLevel     int32
	Cash            int64
	Honra           int64
	Novato          int64
	StorageGold     int64
	BanType         int32
	BanUntil        *time.Time
	AlreadyReset    bool
	Data            []byte
}

type charRow struct {
	Slot            int16
	Name            string
	ClassInfo       int16
	Face            int32
	Evolution       int16
	Level           int32
	Exp             int64
	Gold            int64
	GuildIndex      int32
	GuildMemberType int16
	Fame            int64
	Citizen         int16
	Data            []byte
}

type itemRow struct {
	CharSlot  int16
	Container int16
	Slot      int16
	Item      wire.Item
}

// dateToTime converts a STRUCT_ITEMDATE (local wall clock) to a timestamp.
// A zero year means "unset".
func dateToTime(d wire.ItemDate, loc *time.Location) *time.Time {
	if d.Ano == 0 {
		return nil
	}
	t := time.Date(int(d.Ano), time.Month(d.Mes), int(d.Dia), int(d.Hora), int(d.Minuto), int(d.Segundo), 0, loc)
	return &t
}

func timeToDate(t *time.Time, loc *time.Location) wire.ItemDate {
	if t == nil {
		return wire.ItemDate{}
	}
	l := t.In(loc)
	return wire.ItemDate{
		Dia: uint8(l.Day()), Mes: uint8(l.Month()), Ano: uint16(l.Year()),
		Hora: uint8(l.Hour()), Minuto: uint8(l.Minute()), Segundo: uint8(l.Second()),
	}
}

func itemEmpty(it wire.Item) bool { return it == wire.Item{} }

// splitAccount turns an AccountRecord into rows. Fields that become columns
// or item rows are zeroed in the JSON documents; mergeAccount overlays them
// back, so columns stay authoritative.
func splitAccount(rec *AccountRecord, loc *time.Location) (accountRow, []charRow, []itemRow, error) {
	a := rec.Account // copy

	username := strings.ToUpper(wire.GameString(a.Username[:]))
	if username == "" {
		return accountRow{}, nil, nil, fmt.Errorf("store: empty username")
	}

	row := accountRow{
		Username:     username,
		PasswordHash: rec.PasswordHash,
		Status:       int16(rec.Status),
		PassBlock:    wire.GameString(a.PassBlock[:]),
		AccessLevel:  int32(a.AccessLevel),
		Cash:         int64(a.Cash),
		Honra:        int64(a.Honra),
		Novato:       int64(a.Novato),
		StorageGold:  int64(a.StorageCoin),
		BanType:      int32(a.BanType),
		BanUntil:     dateToTime(a.Ban, loc),
		AlreadyReset: a.AlreadyReset,
	}
	if rec.NumericPassHash != "" {
		h := rec.NumericPassHash
		row.NumericPassHash = &h
	}

	var items []itemRow
	addItems := func(charSlot, container int, src []wire.Item) {
		for i, it := range src {
			if !itemEmpty(it) {
				items = append(items, itemRow{int16(charSlot), int16(container), int16(i), it})
			}
		}
	}
	addItems(storageCharSlot, ContainerStorage, a.StorageItems[:])

	var chars []charRow
	for slot := range a.Mob {
		c := a.Mob[slot] // copy
		if c.Player.Name[0] == 0 {
			continue
		}
		p := &c.Player
		cr := charRow{
			Slot:            int16(slot),
			Name:            wire.GameString(p.Name[:]),
			ClassInfo:       int16(p.ClassInfo),
			Face:            int32(p.Equip[0].Index),
			Evolution:       int16(p.Equip[0].Effect[1].Value),
			Level:           int32(p.BStatus.Level),
			Exp:             p.Exp,
			Gold:            int64(p.Gold),
			GuildIndex:      int32(p.GuildIndex),
			GuildMemberType: int16(p.GuildMemberType),
			Fame:            int64(c.Fame),
			Citizen:         int16(c.Citizen),
		}
		addItems(slot, ContainerEquip, p.Equip[:])
		addItems(slot, ContainerInventory, p.Inventory[:])
		addItems(slot, ContainerFilter, c.FilterDrop.Items[:])
		addItems(slot, ContainerSubEquip, c.Sub.Equip[:])

		p.Name = [16]byte{}
		p.ClassInfo, p.GuildIndex, p.GuildMemberType, p.Gold, p.Exp = 0, 0, 0, 0, 0
		p.BStatus.Level = 0
		p.Equip = [20]wire.Item{}
		p.Inventory = [64]wire.Item{}
		c.Fame, c.Citizen = 0, 0
		c.FilterDrop.Items = [24]wire.Item{}
		c.Sub.Equip = [2]wire.Item{}

		data, err := compactJSON(&c)
		if err != nil {
			return accountRow{}, nil, nil, err
		}
		cr.Data = data
		chars = append(chars, cr)
	}

	a.Username, a.Password, a.SecondPass, a.PassBlock = [16]byte{}, [36]byte{}, [16]byte{}, [16]byte{}
	a.AccessLevel, a.Cash, a.Honra, a.Novato, a.StorageCoin, a.BanType = 0, 0, 0, 0, 0, 0
	a.Ban = wire.ItemDate{}
	a.AlreadyReset = false
	a.StorageItems = [168]wire.Item{}
	a.Mob = [4]wire.CharInfo{}
	data, err := compactJSON(&a)
	if err != nil {
		return accountRow{}, nil, nil, err
	}
	row.Data = data
	return row, chars, items, nil
}

// mergeAccount is the inverse of splitAccount.
func mergeAccount(id int64, row accountRow, chars []charRow, items []itemRow, loc *time.Location) (*AccountRecord, error) {
	rec := &AccountRecord{ID: id, Status: auth.AccountStatus(row.Status), PasswordHash: row.PasswordHash}
	if row.NumericPassHash != nil {
		rec.NumericPassHash = *row.NumericPassHash
	}
	a := &rec.Account
	if err := json.Unmarshal(row.Data, a); err != nil {
		return nil, fmt.Errorf("store: account %s data: %w", row.Username, err)
	}
	wire.PutGameString(a.Username[:], row.Username)
	wire.PutGameString(a.PassBlock[:], row.PassBlock)
	a.AccessLevel = uint32(row.AccessLevel)
	a.Cash, a.Honra, a.Novato = uint32(row.Cash), uint32(row.Honra), uint32(row.Novato)
	a.StorageCoin = uint32(row.StorageGold)
	a.BanType = uint32(row.BanType)
	a.Ban = timeToDate(row.BanUntil, loc)
	a.AlreadyReset = row.AlreadyReset

	for _, cr := range chars {
		if cr.Slot < 0 || cr.Slot > 3 {
			return nil, fmt.Errorf("store: account %s: character slot %d", row.Username, cr.Slot)
		}
		c := &a.Mob[cr.Slot]
		if err := json.Unmarshal(cr.Data, c); err != nil {
			return nil, fmt.Errorf("store: account %s slot %d data: %w", row.Username, cr.Slot, err)
		}
		p := &c.Player
		wire.PutGameString(p.Name[:], cr.Name)
		p.ClassInfo = uint8(cr.ClassInfo)
		p.BStatus.Level = int16(cr.Level)
		p.Exp = cr.Exp
		p.Gold = int32(cr.Gold)
		p.GuildIndex = uint16(cr.GuildIndex)
		p.GuildMemberType = int8(cr.GuildMemberType)
		c.Fame = uint32(cr.Fame)
		c.Citizen = uint8(cr.Citizen)
	}

	for _, ir := range items {
		var dst []wire.Item
		if ir.CharSlot == storageCharSlot {
			dst = a.StorageItems[:]
		} else if ir.CharSlot >= 0 && ir.CharSlot <= 3 {
			c := &a.Mob[ir.CharSlot]
			switch ir.Container {
			case ContainerEquip:
				dst = c.Player.Equip[:]
			case ContainerInventory:
				dst = c.Player.Inventory[:]
			case ContainerFilter:
				dst = c.FilterDrop.Items[:]
			case ContainerSubEquip:
				dst = c.Sub.Equip[:]
			}
		}
		if dst == nil || int(ir.Slot) >= len(dst) {
			return nil, fmt.Errorf("store: account %s: bad item location %d/%d/%d", row.Username, ir.CharSlot, ir.Container, ir.Slot)
		}
		dst[ir.Slot] = ir.Item
	}
	return rec, nil
}

// compactJSON marshals v and drops zero values (0, false, "", and objects
// or arrays that are entirely zero), keeping array positions. Missing keys
// decode back to zero, so this is lossless and keeps jsonb small.
func compactJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, err
	}
	pruned, _ := prune(tree)
	if pruned == nil {
		pruned = map[string]any{}
	}
	return json.Marshal(pruned)
}

// prune returns the value with zero members removed and whether it was zero.
func prune(v any) (any, bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			p, zero := prune(child)
			if zero {
				delete(x, k)
			} else {
				x[k] = p
			}
		}
		return x, len(x) == 0
	case []any:
		allZero := true
		for i, child := range x {
			p, zero := prune(child)
			if zero {
				// keep the position, with the smallest zero of the right kind
				switch child.(type) {
				case map[string]any:
					x[i] = map[string]any{}
				case []any:
					x[i] = []any{}
				default:
					x[i] = child
				}
			} else {
				x[i] = p
				allZero = false
			}
		}
		return x, allZero
	case json.Number:
		return x, x == "0"
	case bool:
		return x, !x
	case string:
		return x, x == ""
	case nil:
		return nil, true
	}
	return v, false
}
