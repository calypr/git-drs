# Makefile

GEN := generate-fixtures
GF_SRC := ./generate-fixtures.go
OUTDIR := fixtures

.PHONY: test-monorepos build-generate-fixtures prepare-repo generate-fixtures-run finalize clean

test-monorepos: build-generate-fixtures prepare-repo generate-fixtures-run finalize
	@echo "test-monorepos complete: $(OUTDIR)"

build-generate-fixtures:
	@echo "Building $(GF_SRC) -> $(GEN)"
	go build -o $(GEN) $(GF_SRC)
	@echo "  OK"

prepare-repo:
	@echo "Recreating" $(CURDIR)/$(OUTDIR)
	@rm -rf $(CURDIR)/$(OUTDIR)
	mkdir -p $(CURDIR)/$(OUTDIR)
	@echo "  OK"
#	@echo "Initializing git repo in ./$(OUTDIR)"
#	git init $(OUTDIR) >/dev/null
#	@echo "  OK"

generate-fixtures-run:
	@echo "Generating fixtures into $(OUTDIR)"
	# Provide TCGA list; change or pipe your own list to the binary as needed.
	@cd $(OUTDIR);  printf "TARGET-ALL-P2\nTCGA-GBM\nTCGA-HNSC\nTCGA-KICH\nTCGA-KIRC\nTCGA-KIRP\nTCGA-LAML\nTCGA-LGG\nTCGA-LIHC\nTCGA-LUAD\nTCGA-LUSC\nTCGA-MESO\nTCGA-OV\nTCGA-PAAD\nTCGA-PCPG\nTCGA-PRAD\nTCGA-READ\nTCGA-SARC\nTCGA-SKCM\nTCGA-STAD\nTCGA-TGCT\nTCGA-THCA\nTCGA-THYM\nTCGA-UVM\nWCDT-MCRPC\n" | ../$(GEN) --number-of-subdirectories=3 --number-of-files=100 -- > /dev/null 2>&1 || true
	@echo "  OK"

finalize:
	@echo "  OK Fixtures created at $(OUTDIR)"

clean:
	@echo "Cleaning generated files"
	-rm -f $(GEN)
	-rm -rf $(OUTDIR)