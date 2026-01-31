package store

import "github.com/calypr/git-drs/drs"

func ObjectPath(basePath string, oid string) (string, error) {
	store := drs.NewObjectStore(basePath, nil)
	return store.ObjectPath(oid)
}

func WriteObject(basePath string, drsObj *drs.DRSObject, oid string) error {
	store := drs.NewObjectStore(basePath, nil)
	return store.WriteObject(drsObj, oid)
}

func ReadObject(basePath string, oid string) (*drs.DRSObject, error) {
	store := drs.NewObjectStore(basePath, nil)
	return store.ReadObject(oid)
}
