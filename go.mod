module github.com/aleybovich/vectrigo

go 1.26.2

replace github.com/aleybovich/bitrace => ./bitrace

replace github.com/aleybovich/minisvg => ./minisvg

require (
	github.com/aleybovich/bitrace v0.0.0-00010101000000-000000000000
	github.com/aleybovich/minisvg v0.0.0-00010101000000-000000000000
	github.com/disintegration/imaging v1.6.2
	github.com/muesli/clusters v0.0.0-20180605185049-a07a36e67d36
	github.com/muesli/kmeans v0.3.1
	golang.org/x/image v0.43.0
)

require (
	github.com/go-json-experiment/json v0.0.0-20240815175050-ebd3a8989ca1 // indirect
	golang.org/x/exp v0.0.0-20240613232115-7f521ea00fb8 // indirect
	honnef.co/go/curve v0.0.0-20260205023122-f94fab6edc34 // indirect
	honnef.co/go/safeish v0.0.0-20241114181457-67c0a2c357ad // indirect
	honnef.co/go/stuff v0.0.0-20251106172302-97592e64bbb7 // indirect
)
