package core

import (
	"errors"
	"fmt"
	"strconv"
	"syscall"
	"unsafe"
)

// Windows Credential Manager through advapi32 (no cgo). A credential blob holds at most
// 2560 bytes, so longer tokens are split across "<key>", "<key>#1", "<key>#2", ...

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	credMaxBlob             = 2560
	errorNotFound           = 1168
)

type winCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

var (
	advapi32       = syscall.NewLazyDLL("advapi32.dll")
	procCredWrite  = advapi32.NewProc("CredWriteW")
	procCredRead   = advapi32.NewProc("CredReadW")
	procCredDelete = advapi32.NewProc("CredDeleteW")
	procCredFree   = advapi32.NewProc("CredFree")
)

type windowsSecrets struct{}

func platformSecrets() secretStore {
	if advapi32.Load() != nil || procCredWrite.Find() != nil {
		return nil
	}
	return windowsSecrets{}
}

func (windowsSecrets) Name() string { return "Windows Credential Manager" }

func target(key string, part int) string {
	t := keychainService + "/" + key
	if part > 0 {
		t += "#" + strconv.Itoa(part)
	}
	return t
}

func credWrite(name string, blob []byte) error {
	tn, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	user, _ := syscall.UTF16PtrFromString("Java4U")
	c := winCredential{Type: credTypeGeneric, TargetName: tn, Persist: credPersistLocalMachine, UserName: user,
		CredentialBlobSize: uint32(len(blob))}
	if len(blob) > 0 {
		c.CredentialBlob = &blob[0]
	}
	r, _, e := procCredWrite.Call(uintptr(unsafe.Pointer(&c)), 0)
	if r == 0 {
		return fmt.Errorf("saving to Credential Manager: %w", e)
	}
	return nil
}

func credRead(name string) ([]byte, error) {
	tn, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	var p *winCredential
	r, _, e := procCredRead.Call(uintptr(unsafe.Pointer(tn)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&p)))
	if r == 0 {
		if errno, ok := e.(syscall.Errno); ok && errno == errorNotFound {
			return nil, errSecretNotFound
		}
		return nil, fmt.Errorf("reading from Credential Manager: %w", e)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(p)))
	out := make([]byte, p.CredentialBlobSize)
	if p.CredentialBlobSize > 0 {
		copy(out, unsafe.Slice(p.CredentialBlob, p.CredentialBlobSize))
	}
	return out, nil
}

func credDelete(name string) error {
	tn, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	r, _, e := procCredDelete.Call(uintptr(unsafe.Pointer(tn)), credTypeGeneric, 0)
	if r == 0 {
		if errno, ok := e.(syscall.Errno); ok && errno == errorNotFound {
			return errSecretNotFound
		}
		return e
	}
	return nil
}

func (windowsSecrets) Set(key, value string) error {
	data := []byte(value)
	parts := 0
	for len(data) > 0 || parts == 0 {
		n := min(len(data), credMaxBlob)
		if err := credWrite(target(key, parts), data[:n]); err != nil {
			return err
		}
		data = data[n:]
		parts++
	}
	// Remove leftover parts from an older, longer value.
	for i := parts; ; i++ {
		if credDelete(target(key, i)) != nil {
			break
		}
	}
	return nil
}

func (windowsSecrets) Get(key string) (string, error) {
	var all []byte
	for i := 0; ; i++ {
		b, err := credRead(target(key, i))
		if errors.Is(err, errSecretNotFound) {
			if i == 0 {
				return "", errSecretNotFound
			}
			break
		}
		if err != nil {
			return "", err
		}
		all = append(all, b...)
		if len(b) < credMaxBlob {
			break
		}
	}
	return string(all), nil
}

func (windowsSecrets) Delete(key string) error {
	for i := 0; ; i++ {
		if credDelete(target(key, i)) != nil {
			return nil
		}
	}
}
