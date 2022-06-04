package engine

import (
	"github.com/go-git/go-git/v5/plumbing/object"
)

func getChangeString(change *object.Change) (*string, error) {
	if change != nil {
		from, _, err := change.Files()
		if err != nil {
			return nil, err
		}
		if from != nil {
			s, err := from.Contents()
			if err != nil {
				return nil, err
			}
			return &s, nil
		}
	}
	return nil, nil
}
