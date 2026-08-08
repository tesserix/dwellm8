import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable, ActivityIndicator } from 'react-native';
import { useRouter } from 'expo-router';
import {
 Avatar, BackHeader, Button, Card, color, count, EmptyState, ErrorState, font, KeyValue,
  Metric, MetricRow, ProgressBar, radius, Screen, space, StatusPill, Toast, useBack,
} from '@dwellm8/mobile-shared';
import { useTeam, type Colleague } from '../src/data/team';
import { usePortfolio } from '../src/data/portfolio';

/**
 * The firm's own team (#353): who it employs, what each carries and who is
 * responsible for which building.
 *
 * A manager handed a tenth building has ten neglected ones, so the load and
 * the room left under the cap are on the card, before anybody is assigned.
 */

export default function Team() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const team = useTeam();
  const portfolio = usePortfolio();

  const [picked, setPicked] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const who = team.working.concat(team.gone).find((m) => m.id === picked);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 3200);
  };

  const attempt = async (what: () => Promise<void>, done: string) => {
    try {
      await what();
      say(done);
    } catch (err) {
      say((err as Error).message);
    }
  };

  // A building somebody already holds is not on offer to anybody else.
  const spoken = new Set(team.working.concat(team.gone).flatMap(
    (m) => m.properties.map((a) => a.property_id)));
  const free = portfolio.rows.filter((p) => !spoken.has(p.id));

  return (
    <>
      <BackHeader
        title="The team"
        subtitle={`${count(team.working.length, 'manager')} on the firm`}
        onBack={goBack}
      />
      <Screen>
        {toast ? <Toast text={toast} /> : null}
        {team.loading ? <View style={s.waiting}><ActivityIndicator /></View> : null}
        {team.error ? <ErrorState error={team.error} onRetry={team.reload} /> : null}

        {!team.loading && !team.error ? (
          <>
            {team.working.length ? (
              <MetricRow>
                <Metric value={String(team.working.length)} label="managers" tone="blue" />
                <Metric
                  value={String(team.working.reduce((n, m) => n + m.held, 0))}
                  label="properties covered"
                  tone="green"
                />
                <Metric
                  value={String(team.working.reduce((n, m) => n + m.spare, 0))}
                  label="capacity spare"
                  tone="amber"
                />
              </MetricRow>
            ) : null}

            {team.working.map((m) => (
              <Person key={m.id} m={m} onPress={() => setPicked(m.id === picked ? null : m.id)} />
            ))}

            {!team.working.length ? (
              <EmptyState
                title="Nobody on the team yet"
                body="Add the managers who look after the buildings. Each one carries a limited number of properties, so nothing goes unattended."
                action="Add a manager"
                onAct={() => router.push('/employ')}
              />
            ) : (
              <View style={s.actions}>
                <Button label="Add a manager" onPress={() => router.push('/employ')} style={{ flex: 1 }} />
                <Button label="Roles" tone="secondary" onPress={() => router.push('/roles')} style={{ flex: 1 }} />
              </View>
            )}

            {who ? (
              <Card>
                <Text style={s.h}>{who.full_name}</Text>
                <View style={{ marginTop: space(3) }}>
                  <KeyValue k="Role" v={who.role_name ?? 'No role set'} />
                  <KeyValue k="Employment" v={who.employment_type?.replace('_', ' ') ?? 'Not stated'} />
                  <KeyValue k="Joined" v={who.joined_on ?? 'Not stated'} />
                  <KeyValue k="PAN" v={who.pan_masked ?? 'Not furnished'} last />
                </View>

                <Text style={[s.h, s.section]}>Responsible for</Text>
                {who.properties.map((a) => (
                  <View key={a.id} style={s.held}>
                    <Text style={s.heldName}>{a.property_name ?? a.property_id}</Text>
                    <Pressable
                      accessibilityRole="button"
                      accessibilityLabel={`Hand back ${a.property_name ?? a.property_id}`}
                      onPress={() => attempt(() => team.release(a.id),
                        `${a.property_name ?? 'That property'} is unassigned`)}
                    >
                      <Text style={s.handBack}>Hand back</Text>
                    </Pressable>
                  </View>
                ))}
                {!who.properties.length ? (
                  <Text style={s.sub}>Nothing yet — give them a building below.</Text>
                ) : null}

                {who.atCapacity ? (
                  <Text style={s.note}>
                    {who.full_name} is carrying as much as their role allows. Hand a building back,
                    or raise what they carry, before adding another.
                  </Text>
                ) : (
                  <>
                    <Text style={[s.h, s.section]}>Give them a building</Text>
                    {free.map((p) => (
                      <Pressable
                        key={p.id}
                        accessibilityRole="button"
                        style={s.offer}
                        onPress={() => attempt(() => team.assign(who.id, p.id),
                          `${p.name} is ${who.full_name}'s`)}
                      >
                        <Text style={s.heldName}>{p.name}</Text>
                      </Pressable>
                    ))}
                    {!free.length ? (
                      <Text style={s.sub}>Every building already has somebody responsible for it.</Text>
                    ) : null}
                  </>
                )}

                <View style={s.actions}>
                  <Button
                    label="Working hours"
                    tone="secondary"
                    onPress={() => router.push({
                      pathname: '/rota', params: { id: who.id, name: who.full_name },
                    })}
                    style={{ flex: 1 }}
                  />
                  <Button
                    label="Record an exit"
                    tone="secondary"
                    onPress={() => attempt(
                      () => team.exit(who.id, new Date().toISOString().slice(0, 10)),
                      `${who.full_name} has left the firm`)}
                    style={{ flex: 1 }}
                  />
                </View>
              </Card>
            ) : null}

            {team.gone.length ? (
              <Card>
                <Text style={s.h}>No longer with the firm</Text>
                {team.gone.map((m) => (
                  <KeyValue key={m.id} k={m.full_name} v={`Left ${m.exited_on ?? ''}`.trim()} />
                ))}
              </Card>
            ) : null}
          </>
        ) : null}
      </Screen>
    </>
  );
}

function Person({ m, onPress }: { m: Colleague; onPress: () => void }) {
  const pct = m.property_limit ? Math.min(100, (m.held / m.property_limit) * 100) : 0;
  return (
    <Card>
      <Pressable accessibilityRole="button" onPress={onPress} style={s.person}>
        <Avatar
          initials={m.full_name.split(/\s+/).map((w) => w[0]).slice(0, 2).join('').toUpperCase()}
          tone={m.atCapacity ? 'amber' : 'blue'}
        />
        <View style={{ flex: 1 }}>
          <Text style={s.h}>{m.full_name}</Text>
          <Text style={s.sub}>{m.designation ?? m.role_name ?? 'No role set'}</Text>
        </View>
        <StatusPill
          text={m.atCapacity ? 'At capacity' : `Room for ${m.spare} more`}
          tone={m.atCapacity ? 'amber' : 'green'}
        />
      </Pressable>
      <View style={{ marginTop: space(3) }}>
        <ProgressBar pct={pct} tint={m.atCapacity ? color.warnInk : color.positive} />
        <Text style={s.load}>{`${m.held} of ${m.property_limit} properties`}</Text>
      </View>
    </Card>
  );
}

const s = StyleSheet.create({
  waiting: { paddingVertical: space(8), alignItems: 'center' },
  h: { ...font.h3, color: color.inkStrong },
  section: { marginTop: space(5), marginBottom: space(2) },
  sub: { ...font.small, color: color.inkSoft, marginTop: 3, lineHeight: 18 },
  note: { ...font.small, color: color.inkFaint, marginTop: space(3), lineHeight: 18 },
  load: { ...font.small, color: color.inkSoft, marginTop: 6 },
  person: { flexDirection: 'row', alignItems: 'center', gap: 12 },
  actions: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(4) },
  held: {
    flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between',
    paddingVertical: space(3), borderBottomWidth: 1, borderBottomColor: color.line,
  },
  heldName: { ...font.body, color: color.inkStrong },
  handBack: { ...font.small, color: color.negative },
  offer: {
    paddingVertical: space(3), paddingHorizontal: space(3), marginTop: space(2),
    borderWidth: 1, borderColor: color.line, borderRadius: radius.md,
  },
});
