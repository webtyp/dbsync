package tests

import (
	"webtyp.com/model"
)

type NoteList []*Note

func (s *NoteList) Len() int                         { return len(*s) }
func (s *NoteList) At(i int) model.Fielder           { return (*s)[i] }
func (s *NoteList) Append() model.Fielder            { v := &Note{}; *s = append(*s, v); return v }
func (s *NoteList) IsNil() bool                      { return s == nil }
func (s *NoteList) EncodeFields(_ model.FieldWriter) {}
func (s *NoteList) DecodeFields(_ model.FieldReader) {}
