#!/bin/sh
# Snapshots for the Family share, laid out as a ZFS NAS with sanoid keeps
# them and Samba's vfs_shadow_copy2 offers them (smb.conf): plain folders in
# .zfs/snapshot named autosnap_<time>_<kind>, hard links to the files that
# did not change, and Fotos a nested dataset with snapshots of its own.
# What they show: Haushalt/Einkaufsliste.txt in two earlier states,
# Rezepte/Linsensuppe.md deleted since, Urlaubsplanung 2027.md new since, a
# blurred birthday photo deleted since. All fictional; run once at build.
set -eu
F=/srv/family

snap() { # dataset, name: a hard-linked copy of the dataset as it is, without its own .zfs and nested ones
	d="$1/.zfs/snapshot/autosnap_$2"
	mkdir -p "$d"
	(cd "$1" && find . -mindepth 1 -maxdepth 1 ! -name .zfs ${3:+! -name "$3"} -exec cp -al {} "$d/" \;)
	echo "$d"
}
write() { # file, mtime, content: a file of its own, never through a hard link
	rm -f "$1"
	printf '%s' "$3" > "$1"
	touch -d "$2" "$1"
}

D1=$(snap "$F" 2026-11-21_03:00:00_daily Fotos)
D2=$(snap "$F" 2026-11-22_15:00:09_hourly Fotos)
for d in "$D1" "$D2"; do
	# In the share's snapshot a nested dataset is an empty folder, as in ZFS.
	mkdir -p "$d/Fotos"
	rm -f "$d/Urlaubsplanung 2027.md"
	write "$d/Rezepte/Linsensuppe.md" '2025-10-04 18:00:00' "# Linsensuppe

- 250 g Linsen
- 1 Zwiebel
- 2 Karotten
"
done
write "$D1/Haushalt/Einkaufsliste.txt" '2025-12-20 09:00:00' "Milch
Brot
"
write "$D2/Haushalt/Einkaufsliste.txt" '2025-12-21 17:30:00' "Milch
Brot
Äpfel
"

P=$(snap "$F/Fotos" 2026-11-01_00:00:01_monthly)
cp -l "$P/Geburtstag Oma/Foto 1.jpg" "$P/Geburtstag Oma/Foto 1 (verwackelt).jpg"

chown -R root:family "$F/.zfs" "$F/Fotos/.zfs"
find "$F/.zfs" "$F/Fotos/.zfs" -type d -exec chmod 2775 {} +
