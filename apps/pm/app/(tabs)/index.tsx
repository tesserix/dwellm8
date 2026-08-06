import React from 'react';
import { View, Text, StyleSheet, Pressable } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Card, CollapsibleHeader, Screen, SectionTitle,
  ProgressBar, StatusPill, Metric, ListRow, Avatar, SyncBadge,
  AlertIcon, BedIcon, BuildingIcon, CalendarIcon, ChartIcon, ChatIcon, ClipboardIcon, DocIcon,
  KeyIcon, RupeeIcon, ShieldIcon, UsersIcon, WrenchIcon,
  color, font, inr, inrShort, radius, space,
} from '@dwellm8/mobile-shared';
import { istDate } from '../../src/data/clock';
import { useOpsTodayData, useOpsWho, useOpsWorklist, type OpsTask } from '../../src/data/source';
import { fmtDate, useOpsApprovals } from '../../src/data/worklists';

/**
 * Today — the manager's first screen of the day.
 *
 * The rule the design follows: what is late, what breaches an SLA and what
 * blocks money comes first; everything else is one tap away. A manager
 * standing in a corridor should be able to act without reading a dashboard.
 */

const kindIcon: Record<string, React.ReactNode> = {
  collect: <RupeeIcon size={20} c={color.negative} />,
  ticket: <WrenchIcon size={20} c="#B0731C" />,
  inspection: <ClipboardIcon size={20} c={color.accent} />,
  approval: <DocIcon size={20} c="#5B4CC4" />,
  lease: <CalendarIcon size={20} c={color.accent} />,
  gate: <ShieldIcon size={20} c={color.accent} />,
};

export default function Today() {
  const router = useRouter();
  const roster = useOpsTodayData();
  const who = useOpsWho();
  const work = useOpsWorklist();
  const { data: approvals } = useOpsApprovals();
  // Every figure comes from the roster, which serves the demonstration set in
  // demo mode and zeroes what has no endpoint yet in live mode (#284, #296).
  const billedPaise = roster.billedPaise;
  const collectedPaise = billedPaise - roster.outstandingPaise;
  const pct = billedPaise > 0 ? Math.round((collectedPaise / billedPaise) * 100) : 0;

  const open = (t: OpsTask) => {
    if (t.kind === 'collect') router.push(`/arrear?id=${t.ref}`);
    else if (t.kind === 'ticket' || t.kind === 'approval') router.push(`/ticket?id=${t.ref}`);
    else router.push('/(tabs)/portfolio');
  };

  return (
    <>
      <AppHeader
        title={who.firmName || 'Your firm'}
        onTitlePress={() => router.push('/switcher')}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
        right={
          <Pressable onPress={() => router.push('/thread')} hitSlop={10}>
            <ChatIcon size={26} />
          </Pressable>
        }
      />
      <Screen>
        <SyncBadge queued={2} />

        <View style={s.greetWrap}>
          <Text style={s.greet}>{who.firstName ? `Good morning, ${who.firstName}` : 'Good morning'}</Text>
          <Text style={s.date}>{istDate()}</Text>
        </View>

        {/* the rent roll and what's outstanding against it — real in live mode */}
        <Card>
          <View style={s.rowBetween}>
            <Text style={s.cardLabel}>{roster.mode === 'live' ? 'Rent roll, position today' : 'Collected in July'}</Text>
            <StatusPill text={`${pct}% collected`} tone={pct >= 90 ? 'green' : 'amber'} />
          </View>
          <Text style={s.big}>{inr(collectedPaise, { noPaise: true })}</Text>
          <ProgressBar pct={pct} tint={pct >= 90 ? color.positive : '#D89A2B'} />
          <Text style={s.sub}>
            {inr(roster.outstandingPaise, { noPaise: true })} outstanding of{' '}
            {inr(billedPaise, { noPaise: true })} billed
          </Text>

          <View style={s.metrics}>
            <Metric
              value={inrShort(roster.outstandingPaise)}
              label={`arrears · ${roster.arrearsCount} tenancies`}
              tone="red"
              onPress={() => router.push('/(tabs)/collect')}
            />
            <Metric
              value={String(roster.payoutsPending)}
              label={`payouts due · ${inrShort(roster.payoutsPaise)}`}
              tone="blue"
              onPress={() => router.push('/payouts')}
            />
          </View>
        </Card>

        <View style={s.metricRow}>
          <Metric
            value={String(roster.openTickets)}
            label={`open jobs · ${roster.breachingSla} breaching`}
            tone={roster.breachingSla ? 'amber' : 'neutral'}
            onPress={() => router.push('/(tabs)/jobs')}
          />
          <Metric
            value={`${roster.visitsDone}/${roster.inspectionsToday}`}
            label="inspections today"
            tone="violet"
            onPress={() => router.push('/(tabs)/inspect')}
          />
          <Metric
            value={`${roster.occupancyPct}%`}
            label={`occupied · ${roster.vacantUnits} vacant`}
            tone="green"
            onPress={() => router.push('/(tabs)/portfolio')}
          />
        </View>

        <CollapsibleHeader title="Your worklist" />
        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {work.tasks.length === 0 ? (
            <Text style={s.empty}>
              {work.loading ? 'Reading your worklist…' : 'Nothing needs you right now.'}
            </Text>
          ) : null}
          {work.tasks.map((t, i) => (
            <ListRow
              key={t.id}
              left={<View style={s.taskIcon}>{kindIcon[t.kind]}</View>}
              title={t.title}
              subtitle={t.where}
              right={
                <View style={{ alignItems: 'flex-end', gap: 5 }}>
                  <Text style={[s.at, t.urgent && { color: color.negative, fontWeight: '800' }]}>{t.at}</Text>
                </View>
              }
              onPress={() => open(t)}
              last={i === work.tasks.length - 1}
              tone={t.urgent ? 'red' : undefined}
            />
          ))}
        </Card>

        {/* An automation stopped at its ceiling and is waiting for a person. */}
        {approvals.length ? (
          <>
            <SectionTitle>Waiting on you</SectionTitle>
            <Card padded={false} style={{ paddingHorizontal: space(4) }}>
              {approvals.map((a, i) => (
                <ListRow
                  key={a.id}
                  left={<Avatar initials={a.automation.slice(0, 2).toUpperCase()} tone="violet" />}
                  title={`${a.automation} — ${inr(a.amount_minor, { noPaise: true })}`}
                  subtitle={`over its ${inr(a.ceiling_minor, { noPaise: true })} ceiling · ${a.subject_kind}`}
                  meta={fmtDate(a.requested_at)}
                  onPress={() => router.push(`/ticket?id=${a.subject_id}`)}
                  last={i === approvals.length - 1}
                />
              ))}
            </Card>
          </>
        ) : null}

        <SectionTitle>Quick actions</SectionTitle>
        <View style={s.quickGrid}>
          <Quick icon={<RupeeIcon size={22} />} label="Record a payment" onPress={() => router.push('/receipt')} />
          <Quick icon={<WrenchIcon size={22} />} label="Log a job" onPress={() => router.push('/(tabs)/jobs')} />
          <Quick icon={<ShieldIcon size={22} />} label="Gate and visitors" onPress={() => router.push('/gate')} />
          <Quick icon={<BedIcon size={22} />} label="Bed allocation" onPress={() => router.push('/beds')} />
          <Quick icon={<UsersIcon size={22} />} label="Leads" onPress={() => router.push('/leads')} />
          <Quick icon={<UsersIcon size={22} />} label="Screening" onPress={() => router.push('/screening')} />
          <Quick icon={<ChartIcon size={22} />} label="Owner payouts" onPress={() => router.push('/payouts')} />
          <Quick icon={<RupeeIcon size={22} />} label="Where rent settles" onPress={() => router.push('/settlement')} />
          <Quick icon={<BuildingIcon size={22} />} label="Society" onPress={() => router.push('/society')} />
          <Quick icon={<ShieldIcon size={22} />} label="Compliance" onPress={() => router.push('/compliance')} />
        </View>

        <View style={s.footNote}>
          <AlertIcon size={16} c={color.inkFaint} />
          <Text style={s.footText}>
            {roster.mode === 'live'
              ? 'Rent roll, arrears, your worklist and approvals are live. Payouts, jobs and occupancy read zero until their endpoints land.'
              : 'Demonstration data.'}
          </Text>
        </View>
      </Screen>
    </>
  );
}

const Quick = ({ icon, label, onPress }: { icon: React.ReactNode; label: string; onPress: () => void }) => (
  <Pressable style={s.quick} onPress={onPress}>
    <View style={s.quickIcon}>{icon}</View>
    <Text style={s.quickText} numberOfLines={2}>{label}</Text>
  </Pressable>
);

const s = StyleSheet.create({
  greetWrap: { paddingHorizontal: space(4), paddingTop: space(4), paddingBottom: space(3) },
  greet: { ...font.h1, color: color.inkStrong },
  date: { ...font.body, color: color.inkSoft, marginTop: 3 },

  rowBetween: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
  cardLabel: { ...font.label, color: color.inkSoft },
  big: { fontSize: 34, fontWeight: '800', color: color.inkStrong, letterSpacing: -0.6, marginTop: 6 },
  sub: { ...font.small, color: color.inkSoft, marginTop: 8 },
  metrics: { flexDirection: 'row', gap: 10, marginTop: space(4) },

  metricRow: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginBottom: space(3) },

  taskIcon: {
    width: 38, height: 38, borderRadius: 19, backgroundColor: '#F3F7FB',
    alignItems: 'center', justifyContent: 'center',
  },
  at: { ...font.small, color: color.inkSoft },
  empty: { ...font.small, color: color.inkSoft, paddingVertical: space(4) },

  quickGrid: { flexDirection: 'row', flexWrap: 'wrap', gap: 10, paddingHorizontal: space(4) },
  quick: {
    width: '31%', backgroundColor: '#FFF', borderRadius: radius.md,
    paddingVertical: space(4), paddingHorizontal: space(2), alignItems: 'center',
    borderWidth: 1, borderColor: color.line,
  },
  quickIcon: { marginBottom: 8 },
  quickText: { ...font.small, color: color.accent, textAlign: 'center', fontWeight: '600' },

  footNote: {
    flexDirection: 'row', alignItems: 'center', gap: 8,
    paddingHorizontal: space(4), paddingTop: space(5),
  },
  footText: { ...font.small, color: color.inkFaint, flex: 1 },
});
