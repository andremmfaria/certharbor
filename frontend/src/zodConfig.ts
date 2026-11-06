import { z } from 'zod'

// Imported first by main.tsx, before any module defines a schema: zod
// otherwise probes for eval with new Function, which script-src 'self' blocks
// and reports as a CSP violation even though zod catches the throw.
z.config({ jitless: true })
