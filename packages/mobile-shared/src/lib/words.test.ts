import { plural, count } from './words';

// A manager reads "1 tenancies" as a bug in the figure, not in the grammar.

it('keeps the singular at one', () => {
  expect(plural(1, 'tenancy', 'tenancies')).toBe('tenancy');
});

it('takes the plural at everything else, nought included', () => {
  expect(plural(0, 'tenancy', 'tenancies')).toBe('tenancies');
  expect(plural(35, 'tenancy', 'tenancies')).toBe('tenancies');
});

it('forms the regular plural without being told it', () => {
  expect(plural(2, 'job')).toBe('jobs');
  expect(plural(1, 'job')).toBe('job');
});

it('counts and names in one phrase', () => {
  expect(count(1, 'tenancy', 'tenancies')).toBe('1 tenancy');
  expect(count(86, 'tenancy', 'tenancies')).toBe('86 tenancies');
});
