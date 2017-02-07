cd daemon
echo "Building Daemon"
go build -v
cd ..
cd cli
echo "Building CLI"
go build -v
echo "Done"