import { definePreset } from '@primeuix/themes'
import Aura from '@primeuix/themes/aura'

// Enterprise look-and-feel preset. Keeps the Aura green brand hue (see brand
// palette decision) but flattens the geometry: near-zero border radius so every
// surface — cards, inputs, buttons, dialogs, menus — gets slightly-sharp edges
// instead of Aura's default rounding. Purely visual; no behaviour changes.
export const TeggoPreset = definePreset(Aura, {
  primitive: {
    borderRadius: {
      none: '0',
      xs: '2px',
      sm: '2px',
      md: '2px',
      lg: '2px',
      xl: '3px',
    },
  },
})
