package ui

import "testing"

// TestPluralFiles проверяет склонение «файл / файла / файлов» в подписях папок.
func TestPluralFiles(t *testing.T) {
	cases := map[int]string{
		0: "0 файлов", 1: "1 файл", 2: "2 файла", 4: "4 файла", 5: "5 файлов",
		11: "11 файлов", 14: "14 файлов", 21: "21 файл", 22: "22 файла",
		100: "100 файлов", 101: "101 файл", 111: "111 файлов",
	}
	for n, want := range cases {
		if got := pluralFiles(n); got != want {
			t.Errorf("pluralFiles(%d) = %q, want %q", n, got, want)
		}
	}
}
