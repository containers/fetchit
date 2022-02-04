# a list of all the books we are analyzing
DATA = glob_wildcards('data/{book}.txt').book

# this is for running on HPC resources
localrules: all, make_archive

# the default rule
rule all:
    input:
        'zipf_analysis.tar.gz'

# count words in one of our books
# logfiles from each run are put in .log files"
rule count_words:
    input:
        wc='source/wordcount.py',
        book='data/{file}.txt'
    output: 'processed_data/{file}.dat'
    threads: 4
    log: 'processed_data/{file}.log'
    shell:
        '''
            python {input.wc} {input.book} {output} >> {log} 2>&1
        '''

# create a plot for each book
rule make_plot:
    input:
        plotcount='source/plotcount.py',
	book='processed_data/{file}.dat'
    output: 'results/{file}.png'
    shell: 'python {input.plotcount} {input.book} {output}'

# generate summary table
rule zipf_test:
    input:
        zipf='source/zipf_test.py',
        books=expand('processed_data/{book}.dat', book=DATA)
    output: 'results/results.txt'
    shell:  'python {input.zipf} {input.books} > {output}'

# create an archive with all of our results
rule make_archive:
    input:
        expand('results/{book}.png', book=DATA),
        expand('processed_data/{book}.dat', book=DATA),
        'results/results.txt'
    output: 'zipf_analysis.tar.gz'
    shell: 'tar -czvf {output} {input}'
