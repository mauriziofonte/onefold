// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Equivalent CLI command of a setup (PowerShell syntax): what the
// form will run, for scripts and for other computers. Display only. The merge
// and rearrange line comes from the server (/api/cli, settings.Args); the
// golden archive lines are built here.
import { RULE_OF } from './regime.js';

const q = (s) => "'" + String(s).replaceAll("'", "''") + "'";

// ruleFlags: the catalog rules turned off (-keep), the size cap, -exclude.
function ruleFlags(rules, exclude) {
  const flags = [];
  const off = Object.entries(RULE_OF).filter(([r, rule]) => rule !== 'big' && rules && rules[r] === false).map(([, rule]) => rule);
  if (off.length) flags.push('-keep ' + off.join(','));
  if (rules && rules.big === false) flags.push('-max-size 0');
  if (exclude && exclude.trim()) flags.push('-exclude ' + q(exclude.trim()));
  return flags;
}

// command returns the PowerShell lines for a wizard setup ({ goal, rooms,
// host, target, verifyFull, videoTier, rules, exclude }) plus the comments;
// line is the server's merge or rearrange command for the other goals.
export function command(s, t, line) {
  const lines = [];
  if (s.goal === 'golden') {
    const host = s.host || 'pc';
    const ins = (s.rooms || []).filter(Boolean);
    lines.push(['onefold catalog', '-host ' + q(host), '-o ' + q(host + '.jsonl'), ...ruleFlags(s.rules, s.exclude), ...ins.map(q)].join(' '));
    lines.push('# ' + t('cli.otherPCs'));
    lines.push(['onefold plan', ...(s.verifyFull ? ['-verify-full'] : []), ...(s.videoTier ? ['-video-tier'] : []), '-o plan.jsonl', q(host + '.jsonl')].join(' '));
    lines.push(['onefold apply --execute', '--target ' + q(s.target || 'X:\\golden'), '--only-host ' + q(host), 'plan.jsonl'].join(' '));
    return lines.join('\n');
  }
  if (!line) return '';
  lines.push(s.goal === 'dupes' ? line + ' -list' : line);
  lines.push('# ' + t(s.goal === 'dupes' ? 'cli.dryOnly' : 'cli.addExecute'));
  return lines.join('\n');
}
