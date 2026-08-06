/** Today, in the timezone the rent is due in — never the device's (#291). */
export function istDate(now: Date = new Date()): string {
  return new Intl.DateTimeFormat('en-IN', {
    timeZone: 'Asia/Kolkata',
    weekday: 'long', day: 'numeric', month: 'long', year: 'numeric',
  }).format(now);
}
