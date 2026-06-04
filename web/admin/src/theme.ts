import { definePreset } from '@primeuix/themes'
import Aura from '@primeuix/themes/aura'

// Enterprise look-and-feel preset. Keeps the Aura green brand hue (see brand
// palette decision) but tightens the geometry to a polished, gently-rounded edge
// (not Aura's softer default, not fully sharp) across every surface — cards,
// inputs, buttons, dialogs, menus. Purely visual; no behaviour changes.
export const TeggoPreset = definePreset(Aura, {
  primitive: {
    borderRadius: {
      none: '0',
      xs: '3px',
      sm: '4px',
      md: '6px',
      lg: '8px',
      xl: '12px',
    },
  },
})
