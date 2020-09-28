set GOARCH=arm
set GOOS=linux
go build -o ..\bin\emu ..\cmd\emu.go

set GOARCH=386
set GOOS=windows
go build -o ..\bin\emu.exe ..\cmd\emu.go