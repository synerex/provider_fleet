# Makefile for Synerex Server

GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
RM=rm


TARGET=fleet-provider
# Main target

.PHONY: build 
build: $(TARGET)

$(TARGET): $(TARGET).go
	$(GOBUILD)

.PHONY: clean
clean: 
	$(RM) $(TARGET)
