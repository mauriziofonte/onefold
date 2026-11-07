# Licensing

onefold is dual-licensed.

## 1. Open source: AGPL-3.0-or-later

Everyone may use onefold under the [GNU Affero General Public License v3.0 or later](LICENSE), free of charge, for any purpose, commercial ones included. In short:

- you may run, study, modify and share it;
- whoever distributes onefold or a modified version must pass on the complete source code under the same license;
- whoever modifies onefold and lets other people use it over a network (a NAS app, a hosted service) must offer those users the source of the modified version;
- every contributor and distributor grants a license to the patents that their changes need (sections 10 and 11 of the license), and loses the license by starting patent litigation over the program.

This is the license of every release, and it stays so: what was published under AGPL-3.0-or-later remains available under it.

## 2. Commercial license

Organizations that cannot accept the AGPL terms, for example to ship onefold inside a closed-source product or service without publishing their changes, can obtain a commercial license from the copyright holder:

Maurizio Fonte, <fonte.maurizio@gmail.com>, subject "onefold commercial license".

A commercial license is a separate written agreement; nothing in this repository grants one.

## 3. Why both work

The copyright holder can offer both licenses because every outside contribution is made under the [Contributor License Agreement](CLA.md), which allows it, while every contribution included in a release also stays available under an open-source license approved by the Open Source Initiative.

## 4. Name and logo

Neither license grants rights to the onefold name or logo: see [TRADEMARKS.md](TRADEMARKS.md).

## 5. Third-party components

The dependencies keep their own licenses. Release binaries ship with `THIRD_PARTY_NOTICES.txt` (`make notices`).

The binaries also carry data made by others: the city table behind the `{city}` and `{country}` placeholders comes from [GeoNames](https://www.geonames.org) under the [Creative Commons Attribution 4.0](https://creativecommons.org/licenses/by/4.0/) license (onefold keeps only part of it: what and how is in [internal/geo/DATA.md](internal/geo/DATA.md)), and the country names from the [Unicode CLDR](https://cldr.unicode.org) under the [Unicode License v3](https://www.unicode.org/license.txt).
