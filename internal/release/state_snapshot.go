package release

import (
	"errors"
	"os"
)

func openStateSnapshot(path string) (*os.File, error) {
	f, err := openSharedState(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 65536 {
		return nil, errors.Join(errors.New("selection state must be a bounded regular file"), err, f.Close())
	}
	return f, nil
}

func readStateJSON(path string, value any) (result error) {
	f, err := openStateSnapshot(path)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, f.Close()) }()
	return decodeJSON(f, value)
}
