# onefold
# Author: Maurizio Fonte <fonte.maurizio@gmail.com>
# Copyright (c) 2026 Maurizio Fonte
# SPDX-License-Identifier: AGPL-3.0-or-later

# Merge two made-up folders: preview, execute, look at the result.
# Rendered by scripts/tapes/render.sh (run inside the sandbox folder).
Output assets/demo/cli-merge.gif
Set Shell bash
Set FontSize 15
Set Width 1100
Set Height 620
Set Padding 16
Set TypingSpeed 35ms

Hide
Type "export PS1='$ ' && clear" Enter
Show

Type "find laptop phone-backup -type f | sort" Enter
Sleep 2.5s
Type "onefold merge -input laptop -input phone-backup -output merged 2>/dev/null" Enter
Sleep 4s
Type "onefold merge -input laptop -input phone-backup -output merged -execute 2>/dev/null | tail -4" Enter
Sleep 3s
Type "clear && find merged -type f -not -path '*/.onefold/*' | sort" Enter
Sleep 4s
