# directory containing source data
SRCDIR := data

# directory containing intermediate data
TMPDIR := processed_data

# results directory
RESDIR := results

# all source files (book texts)
SRCS = $(wildcard $(SRCDIR)/*.txt)

# all intermediate data files
DATA = $(patsubst $(SRCDIR)/%.txt,$(TMPDIR)/%.dat,$(SRCS))

# all images
IMAGES = $(patsubst $(SRCDIR)/%.txt,$(RESDIR)/%.png,$(SRCS))

all: $(DATA) $(IMAGES) $(RESDIR)/results.txt

$(TMPDIR)/%.dat: $(SRCDIR)/%.txt source/wordcount.py
	python source/wordcount.py $< $@

$(RESDIR)/%.png: $(TMPDIR)/%.dat source/plotcount.py
	python source/plotcount.py $< $@

$(RESDIR)/results.txt: $(DATA) source/zipf_test.py
	python source/zipf_test.py $(DATA) > $@

clean:
	@$(RM) $(TMPDIR)/*
	@$(RM) $(RESDIR)/*

.PHONY: clean directories
