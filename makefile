BLDDIR = build
BINARIES = hive-server

all: $(BINARIES)

$(BLDDIR)/%:
	go get .
	go build -o $(BLDDIR)/hive-server .

$(BINARIES): %: $(BLDDIR)/%

clean: 
	rm -rf $(BLDDIR)


.PHONY: all
.PHONY: $(BINARIES)
