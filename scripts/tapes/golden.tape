# onefold
# Author: Maurizio Fonte <fonte.maurizio@gmail.com>
# Copyright (c) 2026 Maurizio Fonte
# SPDX-License-Identifier: AGPL-3.0-or-later

# Photo archive: catalog each machine, plan, preview, apply into a golden tree.
# Rendered by scripts/tapes/render.sh (run inside the sandbox folder).
Output assets/demo/cli-golden.gif
Set Shell bash
Set FontSize 15
Set Width 1100
Set Height 620
Set Padding 16
Set TypingSpeed 35ms

Hide
Type "export PS1='$ ' && clear" Enter
Show

Type "onefold catalog -host laptop -o laptop.jsonl laptop 2>/dev/null | tail -2" Enter
Sleep 2s
Type "onefold catalog -host phone -o phone.jsonl phone-backup 2>/dev/null | tail -2" Enter
Sleep 2s
Type "onefold plan -o plan.jsonl laptop.jsonl phone.jsonl 2>/dev/null | tail -5" Enter
Sleep 4s
Type "clear && onefold apply -with-stats plan.jsonl | tail -8" Enter
Sleep 3.5s
Type "onefold apply -execute -target golden plan.jsonl 2>/dev/null" Enter
Sleep 3s
Type "find golden -type f | sort" Enter
Sleep 4s
