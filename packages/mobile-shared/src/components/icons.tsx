import React from 'react';
import Svg, { Path, Circle, Rect, Line } from 'react-native-svg';
import { color } from '../theme/tokens';

type P = { size?: number; c?: string; w?: number };
const base = ({ size = 24, c = color.accent, w = 1.9 }: P) => ({
  width: size,
  height: size,
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: c,
  strokeWidth: w,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
});

export const HomeIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M3 10.5 12 3l9 7.5" />
    <Path d="M5.5 9.5V20h13V9.5" />
    <Path d="M10 20v-5h4v5" />
  </Svg>
);

export const RupeeIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M7 4h10" />
    <Path d="M7 8.5h10" />
    <Path d="M14.5 4c0 3-2.2 4.5-5 4.5H7l7.5 11.5" />
  </Svg>
);

export const ChatIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M4 5.5h16v10H9l-5 4z" />
    <Path d="M8 9.5h8M8 12.5h5" />
  </Svg>
);

export const WrenchIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M15.5 3.5a5 5 0 0 0-5.8 6.4L3.5 16.1a2 2 0 0 0 2.8 2.8l6.2-6.2a5 5 0 0 0 6.4-5.8l-2.9 2.9-2.8-.7-.7-2.8z" />
  </Svg>
);

export const ClipboardIcon = (p: P) => (
  <Svg {...base(p)}>
    <Rect x="5" y="4.5" width="14" height="16" rx="2.5" />
    <Path d="M9 4.5V3.2A1.2 1.2 0 0 1 10.2 2h3.6A1.2 1.2 0 0 1 15 3.2v1.3z" />
    <Path d="M9 11h6M9 15h4" />
  </Svg>
);

export const DocIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M8 3h6l4 4v12a1.5 1.5 0 0 1-1.5 1.5h-8.5A1.5 1.5 0 0 1 6.5 19V4.5A1.5 1.5 0 0 1 8 3z" />
    <Path d="M14 3v4h4" />
  </Svg>
);

export const CalendarIcon = (p: P) => (
  <Svg {...base(p)}>
    <Rect x="3.5" y="5" width="17" height="15" rx="2.5" />
    <Path d="M3.5 9.5h17M8 3v3.5M16 3v3.5" />
  </Svg>
);

export const UserIcon = (p: P) => (
  <Svg {...base(p)}>
    <Circle cx="12" cy="12" r="9.2" />
    <Circle cx="12" cy="9.8" r="3" />
    <Path d="M6.2 19a6.2 6.2 0 0 1 11.6 0" />
  </Svg>
);

export const ChevronRight = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M9 5l7 7-7 7" />
  </Svg>
);
export const ChevronLeft = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M15 5l-7 7 7 7" />
  </Svg>
);
export const ChevronDown = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M5 9l7 7 7-7" />
  </Svg>
);

export const CloseIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M6 6l12 12M18 6L6 18" />
  </Svg>
);

export const PhoneIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M6.5 4h3l1.5 4-2 1.5a11 11 0 0 0 5.5 5.5l1.5-2 4 1.5v3a2 2 0 0 1-2.2 2A16.5 16.5 0 0 1 4.5 6.2 2 2 0 0 1 6.5 4z" />
  </Svg>
);

export const MailIcon = (p: P) => (
  <Svg {...base(p)}>
    <Rect x="3" y="5.5" width="18" height="13" rx="2.5" />
    <Path d="M3.8 7l8.2 6 8.2-6" />
  </Svg>
);

export const GlobeIcon = (p: P) => (
  <Svg {...base(p)}>
    <Circle cx="12" cy="12" r="9" />
    <Path d="M3 12h18M12 3c2.5 2.6 2.5 15.4 0 18M12 3c-2.5 2.6-2.5 15.4 0 18" />
  </Svg>
);

export const PinIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M20.5 3.5 3.8 10.2l7 2.9 2.9 7z" />
  </Svg>
);

export const SendIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M21 3 3 10.2l7.2 2.9L13.1 21z" />
  </Svg>
);

export const PlusIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M12 5v14M5 12h14" />
  </Svg>
);

export const BellOffIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M8.5 5.5A4 4 0 0 1 16 8c0 4 1.5 5.5 1.5 5.5h-11" />
    <Path d="M10 18.5a2 2 0 0 0 4 0" />
    <Line x1="4" y1="4" x2="20" y2="20" />
  </Svg>
);

export const BedIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M3 17v-6h18v6M3 17v2M21 17v2M3 11V8h7v3" />
  </Svg>
);
export const BathIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M4 12h16v3a4 4 0 0 1-4 4H8a4 4 0 0 1-4-4z" />
    <Path d="M7 12V6a2 2 0 0 1 4 0" />
  </Svg>
);
export const CarIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M4 16v-3l2-5h12l2 5v3" />
    <Path d="M4 16h16v2.5h-3V16M7 18.5H4V16" />
    <Circle cx="7.5" cy="13.5" r="1" />
    <Circle cx="16.5" cy="13.5" r="1" />
  </Svg>
);

export const HouseArt = ({ size = 150 }: { size?: number }) => (
  <Svg width={size} height={size * 0.72} viewBox="0 0 200 144" fill="none">
    <Path d="M28 74 100 22l72 52" stroke={color.inkFaint} strokeWidth="3" strokeLinecap="round" />
    <Path d="M44 66v54h112V66" stroke={color.inkFaint} strokeWidth="3" strokeLinejoin="round" />
    <Rect x="62" y="82" width="24" height="20" rx="3" stroke={color.inkFaint} strokeWidth="3" fill="#FBEFD6" />
    <Path d="M108 120v-26h20v26" stroke={color.inkFaint} strokeWidth="3" />
    <Circle cx="128" cy="88" r="17" stroke="#A9CE9B" strokeWidth="3" fill="#F0F7EC" />
    <Path d="M121 88.5l5 5 9-10" stroke="#6FA95A" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
    <Path d="M150 60v-14" stroke={color.inkFaint} strokeWidth="3" strokeLinecap="round" />
    <Path d="M24 120h152" stroke="#CBD9EC" strokeWidth="4" strokeLinecap="round" />
  </Svg>
);

/* ------------------------------------------------------------ field work */

export const SearchIcon = (p: P) => (
  <Svg {...base(p)}>
    <Circle cx="11" cy="11" r="6.5" />
    <Path d="M15.8 15.8 20 20" />
  </Svg>
);

export const CameraIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M4 8h3l1.6-2.4h6.8L17 8h3v11H4z" />
    <Circle cx="12" cy="13.2" r="3.6" />
  </Svg>
);

export const CheckIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M5 12.5l4.6 4.5L19 7" />
  </Svg>
);

export const CheckCircleIcon = (p: P) => (
  <Svg {...base(p)}>
    <Circle cx="12" cy="12" r="9" />
    <Path d="M8 12.3l2.7 2.7L16 9.5" />
  </Svg>
);

export const AlertIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M12 3.6 21.2 20H2.8z" />
    <Path d="M12 9.5v4.6M12 17.2v.1" />
  </Svg>
);

export const ClockIcon = (p: P) => (
  <Svg {...base(p)}>
    <Circle cx="12" cy="12" r="9" />
    <Path d="M12 7v5.4l3.4 2" />
  </Svg>
);

export const MapPinIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M12 21s6.5-6.1 6.5-10.5a6.5 6.5 0 0 0-13 0C5.5 14.9 12 21 12 21z" />
    <Circle cx="12" cy="10.4" r="2.4" />
  </Svg>
);

export const KeyIcon = (p: P) => (
  <Svg {...base(p)}>
    <Circle cx="8" cy="12" r="4" />
    <Path d="M12 12h9M18 12v3M15.5 12v2.2" />
  </Svg>
);

export const ShieldIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M12 3l7 2.6v6c0 4.4-3 7.7-7 9.4-4-1.7-7-5-7-9.4v-6z" />
    <Path d="M9 12.2l2.2 2.2L15.4 10" />
  </Svg>
);

export const FilterIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M4 6h16M7 12h10M10 18h4" />
  </Svg>
);

export const BuildingIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M4 21V5.5A1.5 1.5 0 0 1 5.5 4h7A1.5 1.5 0 0 1 14 5.5V21" />
    <Path d="M14 10h4.5A1.5 1.5 0 0 1 20 11.5V21M2.5 21h19" />
    <Path d="M7 8h4M7 12h4M7 16h4M16.5 14h1.5M16.5 17.5h1.5" />
  </Svg>
);

export const UsersIcon = (p: P) => (
  <Svg {...base(p)}>
    <Circle cx="9" cy="9" r="3.2" />
    <Path d="M3.5 19a5.5 5.5 0 0 1 11 0" />
    <Path d="M16 6.2a3.2 3.2 0 0 1 0 5.9M17.5 19a5.6 5.6 0 0 0-2-4.3" />
  </Svg>
);

export const ChartIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M4 20V4M4 20h16" />
    <Path d="M8 16.5v-5M12.5 16.5v-9M17 16.5v-3.5" />
  </Svg>
);

export const BellIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M6.5 9.5a5.5 5.5 0 0 1 11 0c0 4 1.5 5.5 1.5 5.5H5s1.5-1.5 1.5-5.5z" />
    <Path d="M10 18.5a2 2 0 0 0 4 0" />
  </Svg>
);

export const RefreshIcon = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M20 11a8 8 0 0 0-13.9-4.6L4 8.5" />
    <Path d="M4 4.5v4h4" />
    <Path d="M4 13a8 8 0 0 0 13.9 4.6L20 15.5" />
    <Path d="M20 19.5v-4h-4" />
  </Svg>
);

export const ArrowUpRight = (p: P) => (
  <Svg {...base(p)}>
    <Path d="M7 17 17 7M9 7h8v8" />
  </Svg>
);

export const RouteIcon = (p: P) => (
  <Svg {...base(p)}>
    <Circle cx="6" cy="6" r="2.6" />
    <Circle cx="18" cy="18" r="2.6" />
    <Path d="M8.6 6H14a3.5 3.5 0 0 1 0 7h-4a3.5 3.5 0 0 0 0 7h5.4" />
  </Svg>
);

export const ToolboxIcon = (p: P) => (
  <Svg {...base(p)}>
    <Rect x="3" y="8.5" width="18" height="11" rx="2.2" />
    <Path d="M8.5 8.5V6.4A1.4 1.4 0 0 1 9.9 5h4.2a1.4 1.4 0 0 1 1.4 1.4v2.1" />
    <Path d="M3 13h18M10.5 11.6h3v2.8h-3z" />
  </Svg>
);
