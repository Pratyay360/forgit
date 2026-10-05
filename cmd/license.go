package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// licenseCmd generates a license template file.
var licenseCmd = &cobra.Command{
	Use:   "license TYPE",
	Short: "Generate a license template",
	Args:  cobra.ExactArgs(1),
	RunE:  runLicense,
}

func init() {
	rootCmd.AddCommand(licenseCmd)

	licenseCmd.Flags().String("year", "", "copyright year (default: current year)")
	licenseCmd.Flags().String("holder", "", "copyright holder")
	licenseCmd.Flags().StringP("output", "o", "", "output file (default: stdout)")
}

func runLicense(cmd *cobra.Command, args []string) error {
	licenseType := args[0]
	year := flagString(cmd, "year")
	if year == "" {
		year = fmt.Sprintf("%d", time.Now().Year())
	}
	holder := flagString(cmd, "holder")
	output := flagString(cmd, "output")

	text, err := licenseTemplate(licenseType, year, holder)
	if err != nil {
		return err
	}

	if output != "" {
		if err := os.WriteFile(output, []byte(text), 0644); err != nil {
			return fmt.Errorf("writing license to %s: %w", output, err)
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", output)
		return nil
	}

	_, err = cmd.OutOrStdout().Write([]byte(text))
	return err
}

func licenseTemplate(t, year, holder string) (string, error) {
	switch t {
	case "mit":
		return mitLicense(year, holder), nil
	case "apache-2.0", "apache":
		return apacheLicense(year, holder), nil
	case "gpl-3.0", "gpl":
		return gpl3License(year, holder), nil
	case "bsd-3-clause", "bsd":
		return bsd3License(year, holder), nil
	case "isc":
		return iscLicense(year, holder), nil
	case "mpl-2.0", "mpl":
		return mpl2License(year, holder), nil
	case "unlicense":
		return unlicenseText(), nil
	default:
		return "", fmt.Errorf("unknown license type %q; supported: mit, apache-2.0, gpl-3.0, bsd-3-clause, isc, mpl-2.0, unlicense", t)
	}
}

func mitLicense(year, holder string) string {
	holderLine := holder
	if holder == "" {
		holderLine = "[copyright holder]"
	}
	return fmt.Sprintf(`MIT License

Copyright (c) %s %s

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`, year, holderLine)
}

func apacheLicense(year, holder string) string {
	holderLine := holder
	if holder == "" {
		holderLine = "[copyright holder]"
	}
	return fmt.Sprintf(`Apache License
Version 2.0, January 2004
http://www.apache.org/licenses/

TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION

1. Definitions.

"License" shall mean the terms and conditions for use, reproduction,
and distribution as defined by Sections 1 through 9 of this document.

"Licensor" shall mean the copyright owner or entity authorized by
the copyright owner that is granting the License.

"Legal Entity" shall mean the union of the acting entity and all
other entities that control, are controlled by, or are under common
control with that entity. For the purposes of this definition,
"control" means (i) the power, direct or indirect, to cause the
direction or management of such entity, whether by contract or
otherwise, or (ii) ownership of fifty percent (%%50%%) or more of the
outstanding shares, or (iii) beneficial ownership of such entity.

"You" (or "Your") shall mean an individual or Legal Entity
exercising permissions granted by this License.

"Source" form shall mean the preferred form for making modifications,
including but not limited to software source code, documentation
source, and configuration files.

"Object" form shall mean any form resulting from mechanical
transformation or translation of a Source form, including but
limited to compiled object code, generated documentation,
and conversions to other media types.

"Work" shall mean the work of authorship, whether in Source or
Object form, made available under the License, as indicated by a
copyright notice that is included in or attached to the work
(an example is provided in the Appendix below).

"Derivative Works" shall mean any work, whether in Source or Object
form, that is based on (or derived from) the Work and for which the
editorial revisions, annotations, elaborations, or other modifications
represent, as a whole, an original work of authorship. For the purposes
of this License, Derivative Works shall not include works that remain
separable from, or merely link (or bind by name) to the interfaces of,
the Work and Derivative Works thereof.

"Contribution" shall mean any work of authorship, including
the original version of the Work and any modifications or additions
to that Work or Derivative Works thereof, that is intentionally
submitted to the Licensor for inclusion in the Work by the copyright owner
or by an individual or Legal Entity authorized to submit on behalf of
the copyright owner. For the purposes of this definition, "submitted"
means any form of electronic, verbal, or written communication sent
to the Licensor or its representatives, including but not limited to
communication on electronic mailing lists, source code control systems,
and issue tracking systems that are managed by, or on behalf of, the
Licensor for the purpose of discussing and improving the Work, but
excluding communication that is conspicuously marked or otherwise
designated in writing by the copyright owner as "Not a Contribution."

"Contributor" shall mean Licensor and any individual or Legal Entity
on behalf of whom a Contribution has been received by Licensor and
subsequently incorporated within the Work.

2. Grant of Copyright License. Subject to the terms and conditions of
this License, each Contributor hereby grants to You a perpetual,
worldwide, non-exclusive, no-charge, royalty-free, irrevocable
copyright license to reproduce, prepare Derivative Works of,
publicly display, publicly perform, sublicense, and distribute the
Work and such Derivative Works in Source or Object form.

3. Grant of Patent License. Subject to the terms and conditions of
this License, each Contributor hereby grants to You a perpetual,
worldwide, non-exclusive, no-charge, royalty-free, irrevocable
(except as stated in this section) patent license to make, have made,
use, offer to sell, sell, import, and otherwise transfer the Work,
where such license applies only to those patent claims licensable
by such Contributor that are necessarily infringed by their
Contribution(s) alone or by combination of their Contribution(s)
with the Work to which such Contribution(s) was submitted. If You
institute patent litigation against any entity (including a
cross-claim or counterclaim in a lawsuit) alleging that the Work
or a Contribution incorporated within the Work constitutes direct
or contributory patent infringement, then any patent licenses
granted to You under this License for that Work shall terminate
as of the date such litigation is filed.

4. Redistribution. You may reproduce and distribute copies of the
Work or Derivative Works thereof in any medium, with or without
modifications, and in Source or Object form, provided that You
meet the following conditions:

(a) You must give any other recipients of the Work or
Derivative Works a copy of this License; and

(b) You must cause any modified files to carry prominent notices
stating that You changed the files; and

(c) You must retain, in the Source form of any Derivative Works
that You distribute, all copyright, patent, trademark, and
attribution notices from the Source form of the Work,
excluding those notices that do not pertain to any part of
the Derivative Works; and

(d) If the Work includes a "NOTICE" text file as part of its
distribution, then any Derivative Works that You distribute must
include a readable copy of the attribution notices contained
within such NOTICE file, excluding those notices that do not
pertain to any part of the Derivative Works, in at least one
of the following places: within a NOTICE text file distributed
as part of the Derivative Works; within the Source form or
documentation, if provided along with the Derivative Works; or,
within a display generated by the Derivative Works, if and
wherever such third-party notices normally appear. The contents
of the NOTICE file are for informational purposes only and
do not modify the License. You may add Your own attribution
notices within Derivative Works that You distribute, alongside
or as an addendum to the NOTICE text from the Work, provided
that such additional attribution notices cannot be construed
as modifying the License.

You may add Your own copyright statement to Your modifications and
may provide additional or different license terms and conditions
for use, reproduction, or distribution of Your modifications, or
for any such Derivative Works as a whole, provided Your use,
reproduction, and distribution of the Work otherwise complies with
the conditions stated in this License.

5. Submission of Contributions. Unless You explicitly state otherwise,
any Contribution intentionally submitted for inclusion in the Work
by You to the Licensor shall be under the terms and conditions of
this License, without any additional terms or conditions.
Notwithstanding the above, nothing herein shall supersede or modify
the terms of any separate license agreement you may have executed
with Licensor regarding such Contributions.

6. Trademarks. This License does not grant permission to use the trade
names, trademarks, service marks, or product names of the Licensor,
except as required for reasonable and customary use in describing
the origin of the Work and reproducing the content of the NOTICE file.

7. Disclaimer of Warranty. Unless required by applicable law or
agreed to in writing, Licensor provides the Work (and each
Contributor provides its Contributions) on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied, including, without limitation, any warranties or conditions
of TITLE, NON-INFRINGEMENT, MERCHANTABILITY, or FITNESS FOR A
PARTICULAR PURPOSE. You are solely responsible for determining the
appropriateness of using or redistributing the Work and assume any
risks associated with Your exercise of permissions under this License.

8. Limitation of Liability. In no event and under no legal theory,
whether in tort (including negligence), contract, or otherwise,
unless required by applicable law (such as deliberate and grossly
negligent acts) or agreed to in writing, shall any Contributor be
liable to You for damages, including any direct, indirect, special,
incidental, or consequential damages of any character arising as a
result of this License or out of the use or inability to use the
Work (including but not limited to damages for loss of goodwill,
work stoppage, computer failure or malfunction, and any and all
other commercial damages or losses), even if such Contributor
has been advised of the possibility of such damages.

9. Accepting Warranty or Additional Liability. While redistributing
the Work or Derivative Works thereof, You may choose to offer,
and charge a fee for, acceptance of support, warranty, indemnity,
or other liability obligations and/or rights consistent with this
License. However, in accepting such obligations, You may act only
on Your own behalf and on Your sole responsibility, not on behalf
of any other Contributor, and only if You agree to indemnify,
defend, and hold each Contributor harmless for any liability
incurred by, or claims asserted against, such Contributor by reason
of your accepting any such warranty or additional liability.

END OF TERMS AND CONDITIONS

APPENDIX: How to apply the Apache License to your work.

To apply the Apache License to your work, attach the following
boilerplate notice, with the fields enclosed by brackets "[]"
replaced with your own identifying information. (Don't include
the brackets!)  The text should be enclosed in the appropriate
comment syntax for the file format. We also recommend that a
file or class name and description of purpose be included on the
same "printed page" as the copyright notice for easier
identification within third-party archives.

Copyright %s %s

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
`, year, holderLine, year, holderLine)
}

func gpl3License(year, holder string) string {
	holderLine := holder
	if holder == "" {
		holderLine = "[copyright holder]"
	}
	return fmt.Sprintf(`GNU GENERAL PUBLIC LICENSE
Version 3, 29 June 2007

Copyright (C) %s %s

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
`, year, holderLine)
}

func bsd3License(year, holder string) string {
	holderLine := holder
	if holder == "" {
		holderLine = "[copyright holder]"
	}
	return fmt.Sprintf(`BSD 3-Clause License

Copyright (c) %s %s. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

* Redistributions of source code must retain the above copyright
  notice, this list of conditions and the following disclaimer.

* Redistributions in binary form must reproduce the above copyright
  notice, this list of conditions and the following disclaimer in the
  documentation and/or other materials provided with the distribution.

* Neither the name of the copyright holder nor the names of its
  contributors may be used to endorse or promote products derived from
  this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
`, year, holderLine)
}

func iscLicense(year, holder string) string {
	holderLine := holder
	if holder == "" {
		holderLine = "[copyright holder]"
	}
	return fmt.Sprintf(`ISC License

Copyright (c) %s %s

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
`, year, holderLine)
}

func mpl2License(year, holder string) string {
	holderLine := holder
	if holder == "" {
		holderLine = "[copyright holder]"
	}
	return fmt.Sprintf(`Mozilla Public License Version 2.0

Copyright %s %s

1. Definitions

   "Contributor" means each individual or legal entity that creates,
   contributes to the creation of, or owns Covered Software.

   "Contributor Version" means the combination of the Contributions of
   others (if any) used by a Contributor and that particular Contributor's
   Contribution.

   "Contribution" means Covered Software of a particular Contributor.

   "Covered Software" means Source Code Form to which the initial
   Contributor has attached the notice in Section 3.1 and for which such
   Contributor grants you a license under this License.

   "Incompatible With Secondary Licenses" means either:
     (i) the majority of Licenses specified in Section 9.1(b)(1) through
     Section 9.1(b)(10); or
     (ii) those Licenses specified in Section 9.1(b)(11) and Section 9.1(b)(12).

   "Legal Entity" means the union of the acting entity and all other entities
   that control, are controlled by, or are under common control with that
   entity. For purposes of this definition, "control" means (i) the power,
   direct or indirect, to cause the direction or management of such entity,
   whether by contract or otherwise, or (ii) ownership of fifty percent (%%50%%)
   or more of the outstanding shares, or (iii) beneficial ownership of such
   entity.

   "You" (or "Your") means an individual or a legal entity exercising
   permissions under this License. For legal entities, "You" includes any
   entity that controls, is controlled by, or is under common control with
   You.

2. License Grants and Conditions

   For each Contribution a Contributor grants you a perpetual, worldwide,
   non-exclusive, no-charge, royalty-free, irrevocable copyright license to
   reproduce, prepare Derivative Works of, publicly display, publicly perform,
   sublicense, and distribute the Contribution and such Derivative Works in
   Source and Object form.

   For each Contribution a Contributor grants you a perpetual, worldwide,
   non-exclusive, no-charge, royalty-free, irrevocable patent license to make,
   have made, use, offer to sell, sell, import, and otherwise transfer the
   Contribution, where such license applies only to those patent claims
   licensable by such Contributor that are necessarily infringed by such
   Contribution.

   The licenses granted are subject to Section 2 conditions.

3. Source Code Form

   The "Source Code Form" of a Contribution is the form preferred for making
   modifications.

4. Confidential Information

   If You agree to add a notice to Covered Software stating that You
   redistribute Covered Software under this License, the notices described
   herein in Sections 3.1 and 3.2 may be included in the Source Code Form.

5. Distribution of Source Code

   You must distribute Covered Software in Source Code Form under this
   License and must not distribute it under any other license.

6. Distribution of Executable Form

   If You distribute Covered Software in Executable Form then You may do so
   under this License or under the terms of a Secondary License specified in
   Section 9.1 if the Source Code Form is made available under the terms of
   a Secondary License.

7. Distribution of Covered Software in Aggregation

   This License does not grant you permission to distribute Covered Software
   in any form of an aggregate where such form does not produce copyright
   notices in the Covered Software.

8. Trademarks

   This License does not grant permission to use the trade names, trademarks,
   service marks, or product names of a Contributor, except as required to
   reproduce the content of the NOTICE file.

9. Multiple Licensing

   Covered Software may be made available under different licenses, and you
   may choose to distribute Covered Software under one of the following:

   (a) the terms of this License or any Secondary License; or

   (b) any licenses of your choice that are approved as Secondary Licenses
       by the Mozilla Foundation.

10. Terms for Secondary Licenses

   If Covered Software is made available under Secondary Licenses the
   Contributor may elect to grant permission to use the Covered Software
   under the terms of any Secondary License.

11. Disclaimer of Warranty

   Covered Software is provided under this License on an "as is" basis,
   without warranty of any kind, either express, implied, or statutory,
   including, without limitation, warranties that the Covered Software is
   free of claims, or that it is free of infringement, or that it can be
   used without infringement by third parties.

   Contributors specifically disclaim any warranties of merchantability,
   fitness for a particular purpose and non-infringement.

12. Limitation of Liability

   In no circumstances and under no legal theory, whether in tort (including
   negligence), contract, or otherwise, shall any Contributor be liable to
   You for any direct, indirect, special, incidental, special, exemplary,
   or consequential damages of any character including, without limitation,
   procurement of substitute goods or services; loss of use, data, or profits;
   or business interruption, however caused and on any theory of liability,
   whether in contract, strict liability, or tort (including negligence or
   otherwise) arising in any way out of the use of this software, even if
   advised of the possibility of such damages.

13. Intellectual Property

   All patent, trademark, and copyright licenses granted hereunder are
   non-exclusive, transferable, revocable, and (except as stated in this
   section) unlimited.

14. Terms for Patent Claims

   If You institute patent litigation against any entity (including a
   cross-claim or counterclaim in a lawsuit) alleging that the Work or a
   Contribution incorporated within the Work constitutes direct or
   contributory patent infringement, then any patent licenses granted to
   You under this License for that Work shall terminate as of the date
   such litigation is filed.

15. Termination

   This License and the rights granted hereunder will terminate
   automatically if You fail to comply with its terms. However, if You
   become compliant, then the licenses granted hereunder are reinstated
   on a perpetual basis.

16. Miscellaneous

   This License represents the complete agreement concerning the subject
   matter hereof. If any provision of this License is held to be
   unenforceable, such provision shall be reformed only to the extent
   necessary to make it enforceable.

Copyright %s %s

   This Source Code Form is subject to the terms of the Mozilla Public
   License, v. 2.0. If a copy of the MPL was not distributed with this
   file, You can obtain one at https://mozilla.org/MPL/2.0/.
`, year, holderLine, year, holderLine)
}

func unlicenseText() string {
	return `This is free and unencumbered software released into the public domain.

Anyone is free to copy, modify, publish, use, compile, sell, or
distribute this software, either in source code form or as a compiled
binary, for any purpose, commercial or non-commercial, and by any
means.

In jurisdictions that recognize copyright laws, the author or authors
of this software dedicate any and all copyright interest in the
software to the public domain. We make this dedication for the benefit
of the public at large and to the detriment of our heirs and
successors. We intend this dedication to be an overt act of
relinquishment in perpetuity of all present and future rights to this
software under copyright law.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
IN NO EVENT SHALL THE AUTHORS BE LIABLE FOR ANY CLAIM, DAMAGES OR
OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE,
ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
OTHER DEALINGS IN THE SOFTWARE.

For more information, please refer to <https://unlicense.org/>
`
}
