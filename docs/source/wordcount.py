import sys

DELIMITERS = ". , ; : ? $ @ ^ < > # % ` ! * - = ( ) [ ] { } / \" '".split()


def load_text(filename):
    """
    Load lines from a plain-text file and return these as a list, with
    trailing newlines stripped.
    """
    with open(filename, "r") as input_fd:
        lines = input_fd.read().splitlines()
    return lines


def save_word_counts(filename, counts):
    """
    Save a list of [word, count, percentage] lists to a file, in the form
    "word count percentage", one tuple per line.
    """
    with open(filename, 'w') as output:
        for count in counts:
            output.write("%s\n" % " ".join(str(c) for c in count))


def load_word_counts(filename):
    """
    Load a list of (word, count, percentage) tuples from a file where each
    line is of the form "word count percentage". Lines starting with # are
    ignored.
    """
    counts = []
    with open(filename, "r") as input_fd:
        for line in input_fd:
            if not line.startswith("#"):
                fields = line.split()
                counts.append((fields[0], int(fields[1]), float(fields[2])))
    return counts


def update_word_counts(line, counts):
    """
    Given a string, parse the string and update a dictionary of word
    counts (mapping words to counts of their frequencies). DELIMITERS are
    removed before the string is parsed. The function is case-insensitive
    and words in the dictionary are in lower-case.
    """
    for purge in DELIMITERS:
        line = line.replace(purge, " ")
    words = line.split()
    for word in words:
        word = word.lower().strip()
        if word in counts:
            counts[word] += 1
        else:
            counts[word] = 1


def calculate_word_counts(lines):
    """
    Given a list of strings, parse each string and create a dictionary of
    word counts (mapping words to counts of their frequencies). DELIMITERS
    are removed before the string is parsed. The function is
    case-insensitive and words in the dictionary are in lower-case.
    """
    counts = {}
    for line in lines:
        update_word_counts(line, counts)
    return counts


def word_count_dict_to_tuples(counts, decrease=True):
    """
    Given a dictionary of word counts (mapping words to counts of their
    frequencies), convert this into an ordered list of tuples (word,
    count). The list is ordered by decreasing count, unless increase is
    True.
    """
    return sorted(list(counts.items()), key=lambda key_value: key_value[1],
                  reverse=decrease)


def filter_word_counts(counts, min_length=1):
    """
    Given a list of (word, count) tuples, create a new list with only
    those tuples whose word is >= min_length.
    """
    stripped = []
    for (word, count) in counts:
        if len(word) >= min_length:
            stripped.append((word, count))
    return stripped


def calculate_percentages(counts):
    """
    Given a list of (word, count) tuples, create a new list (word, count,
    percentage) where percentage is the percentage number of occurrences
    of this word compared to the total number of words.
    """
    total = 0
    for count in counts:
        total += count[1]
    tuples = [(word, count, (float(count) / total) * 100.0)
              for (word, count) in counts]
    return tuples


def word_count(input_file, output_file, min_length=1):
    """
    Load a file, calculate the frequencies of each word in the file and
    save in a new file the words, counts and percentages of the total  in
    descending order. Only words whose length is >= min_length are
    included.
    """
    lines = load_text(input_file)
    counts = calculate_word_counts(lines)
    sorted_counts = word_count_dict_to_tuples(counts)
    sorted_counts = filter_word_counts(sorted_counts, min_length)
    percentage_counts = calculate_percentages(sorted_counts)
    save_word_counts(output_file, percentage_counts)


if __name__ == '__main__':
    input_file = sys.argv[1]
    output_file = sys.argv[2]
    min_length = 1
    if len(sys.argv) > 3:
        min_length = int(sys.argv[3])
    word_count(input_file, output_file, min_length)
