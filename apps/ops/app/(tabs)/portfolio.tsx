import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter, type Href } from 'expo-router';
import {
  AppHeader, AvatarButton, Button, Card, Screen, SearchBar, ListRow, StatusPill, Metric,
  SectionTitle, BedIcon, BuildingIcon, ClipboardIcon, PlusIcon, RefreshIcon, ShieldIcon, UsersIcon, ChartIcon,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { propertiesOps, today } from '../../src/data/mock';
import { checklists } from '../../src/data/checklists';
import { approvals, automations, enabledCount } from '../../src/data/automations';

/**
 * Portfolio — properties, units and who is in them.
 *
 * The manager's mental model is the unit, not the property, so a property
 * expands straight into its units with tenancy state on each row.
 */

const unitTone: Record<string, Tone> = {
  Occupied: 'green',
  Vacant: 'amber',
  Notice: 'violet',
  'Under repair': 'red',
};

const kindIcon: Record<string, React.ReactNode> = {
  Residential: <BuildingIcon size={20} />,
  Hostel: <BedIcon size={20} />,
  Commercial: <BuildingIcon size={20} />,
  Society: <ShieldIcon size={20} />,
};

export default function Portfolio() {
  const router = useRouter();
  const [q, setQ] = useState('');
  const [open, setOpen] = useState<string | null>('pr-bpg');

  const list = propertiesOps.filter(
    (p) =>
      !q ||
      p.name.toLowerCase().includes(q.toLowerCase()) ||
      p.units.some((u) => (u.tenant ?? '').toLowerCase().includes(q.toLowerCase())),
  );

  const totalUnits = propertiesOps.reduce((a, p) => a + p.units.length, 0);
  const vacant = propertiesOps.reduce((a, p) => a + p.units.filter((u) => u.status === 'Vacant').length, 0);
  const roll = propertiesOps.reduce((a, p) => a + p.monthlyRentPaise, 0);
  const openProcesses = checklists.filter((c) => c.state === 'open').length;
  const lateProcesses = checklists.filter((c) => c.state === 'open' && c.daysOverdue > 0).length;
  const pendingApprovals = approvals.length;

  return (
    <>
      <AppHeader
        title="Portfolio"
        showCaret={false}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
      />
      <Screen>
        <View style={s.metrics}>
          <Metric value={String(totalUnits)} label="units managed" tone="blue" />
          <Metric value={String(vacant)} label="vacant now" tone="amber" />
          <Metric value={`${today.occupancyPct}%`} label="occupancy" tone="green" />
        </View>

        <Button
          label="Onboard a new owner"
          icon={<PlusIcon size={19} c="#FFF" />}
          // The route union regenerates on the next expo start; the cast
          // bridges until then.
          onPress={() => router.push('/onboard' as Href)}
          style={{ marginHorizontal: space(4), marginTop: space(3) }}
        />

        <Card>
          <Text style={s.rollLabel}>Monthly rent roll</Text>
          <Text style={s.roll}>{inr(roll, { noPaise: true })}</Text>
          <Text style={s.rollSub}>Across {propertiesOps.length} properties and 3 owners</Text>
        </Card>

        <SearchBar value={q} onChange={setQ} placeholder="Property, unit or tenant" />

        {list.map((p) => {
          const isOpen = open === p.id;
          return (
            <Card key={p.id} padded={false} style={{ paddingHorizontal: space(4), paddingVertical: space(2) }}>
              <ListRow
                left={<View style={s.pIcon}>{kindIcon[p.kind]}</View>}
                title={p.name}
                subtitle={`${p.locality}`}
                meta={`${p.units.length} units · owner ${p.owner}${p.openTickets ? ` · ${p.openTickets} open jobs` : ''}`}
                right={<StatusPill text={p.kind} tone="neutral" />}
                onPress={() => setOpen(isOpen ? null : p.id)}
                last
              />
              {isOpen ? (
                <View style={s.units}>
                  {p.units.map((u, i) => (
                    <ListRow
                      key={u.id}
                      title={`${u.label}${u.tenant ? ` — ${u.tenant}` : ''}`}
                      subtitle={`${inr(u.rentPaise, { noPaise: true })} per month${
                        u.paidTo ? ` · paid to ${u.paidTo}` : ''
                      }`}
                      meta={u.leaseEnds ? `Lease ends ${u.leaseEnds}` : u.status === 'Vacant' ? 'Available to let' : undefined}
                      right={<StatusPill text={u.status} tone={unitTone[u.status]} />}
                      onPress={() => router.push(`/property?id=${p.id}&unit=${u.id}`)}
                      last={i === p.units.length - 1}
                    />
                  ))}
                </View>
              ) : null}
            </Card>
          );
        })}

        <SectionTitle>Elsewhere</SectionTitle>
        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          <ListRow
            left={<RefreshIcon size={20} />}
            title="Automations"
            subtitle={`${enabledCount(automations)} of ${automations.length} running${pendingApprovals ? ` · ${pendingApprovals} need approval` : ''}`}
            onPress={() => router.push('/automations')}
          />
          <ListRow
            left={<ClipboardIcon size={20} />}
            title="Processes"
            subtitle={`${openProcesses} under way · ${lateProcesses} running late`}
            onPress={() => router.push('/processes')}
          />
          <ListRow
            left={<BedIcon size={20} />}
            title="Bed allocation — Nest PG"
            subtitle="48 beds, 5 vacant, 1 on notice"
            onPress={() => router.push('/beds')}
          />
          <ListRow
            left={<ShieldIcon size={20} />}
            title="Gate and visitors"
            subtitle="2 passes waiting on you"
            onPress={() => router.push('/gate')}
          />
          <ListRow
            left={<UsersIcon size={20} />}
            title="Leads and viewings"
            subtitle="4 active, 1 offer out"
            onPress={() => router.push('/leads')}
          />
          <ListRow
            left={<ChartIcon size={20} />}
            title="Owner payouts"
            subtitle="2 ready to release, 2 blocked"
            onPress={() => router.push('/payouts')}
          />
          <ListRow
            left={<BuildingIcon size={20} />}
            title="Society — Brigade Palm Grove RWA"
            subtitle="87% of dues collected, 9 flats in arrears"
            onPress={() => router.push('/society')}
          />
          <ListRow
            left={<ShieldIcon size={20} />}
            title="Compliance register"
            subtitle="1 expired, 2 due within 60 days"
            onPress={() => router.push('/compliance')}
            last
          />
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(4), marginBottom: space(3) },
  rollLabel: { ...font.label, color: color.inkSoft },
  roll: { fontSize: 30, fontWeight: '800', color: color.inkStrong, letterSpacing: -0.5, marginTop: 4 },
  rollSub: { ...font.small, color: color.inkSoft, marginTop: 4 },
  pIcon: {
    width: 38, height: 38, borderRadius: 19, backgroundColor: '#F3F7FB',
    alignItems: 'center', justifyContent: 'center',
  },
  units: { backgroundColor: '#FAFCFE', borderRadius: 12, paddingHorizontal: space(3), marginBottom: space(2) },
});
