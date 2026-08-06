/**
 * @dwellm8/mobile-shared
 *
 * One implementation of the design system, the icon set, the app shell and the
 * money formatters, consumed by every Dwellm8 app. A fix lands once.
 *
 * The rule that keeps it honest: an app may not reimplement anything exported
 * here, and nothing here may import from an app.
 */
export * from './theme/tokens';
export * from './components/icons';
export * from './components/ui';
export * from './components/controls';
export * from './components/Shell';
export * from './lib/money';
export * from './lib/tds';
export * from './lib/api';
export * from './lib/auth';
export * from './lib/pack';
