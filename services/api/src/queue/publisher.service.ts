import { Injectable, Logger, OnApplicationBootstrap, OnApplicationShutdown } from '@nestjs/common';
import { connect, type ChannelModel, type ConfirmChannel } from 'amqplib';

/**
 * Publisher for the build/run job topology (plan §6).
 * definitions.json is the source of truth — we only passively verify it and
 * never redeclare it.
 */
@Injectable()
export class PublisherService implements OnApplicationBootstrap, OnApplicationShutdown {
  private readonly logger = new Logger(PublisherService.name);
  private connection?: ChannelModel;
  private channel?: ConfirmChannel;

  async onApplicationBootstrap(): Promise<void> {
    const url = process.env.AMQP_URL;
    if (!url) throw new Error('AMQP_URL is not set');
    this.connection = await connect(url);
    this.channel = await this.connection.createConfirmChannel();
    await this.channel.checkExchange('build.x'); // throws if definitions.json was never imported
    await this.channel.checkQueue('build.q');
    this.channel.on('return', (msg) =>
      this.logger.error(`unroutable message returned: ${msg.fields.exchange}/${msg.fields.routingKey}`),
    );
    this.connection.on('error', (err) => this.logger.error(`amqp connection error: ${err.message}`));
    this.logger.log('publisher connected, build topology verified');
    // ponytail: no auto-reconnect — a dropped connection surfaces as publish
    // errors (uploads → failed) and compose restarts the api if it dies at boot.
  }

  async publishBuild(deploymentId: string): Promise<void> {
    await this.publish('build.x', 'build', { type: 'build', deployment_id: deploymentId });
  }

  /** Action job (plan §8.6): routed exactly like a deploy — run.x keyed by the project's node. */
  async publishAction(actionId: string, nodeId: string): Promise<void> {
    await this.publish('run.x', nodeId, { type: 'action', action_id: actionId });
  }

  private async publish(exchange: string, routingKey: string, body: unknown): Promise<void> {
    const channel = this.channel;
    if (!channel) throw new Error('publisher not connected');
    channel.publish(exchange, routingKey, Buffer.from(JSON.stringify(body)), {
      persistent: true,
      contentType: 'application/json',
      mandatory: true, // unroutable → basic.return instead of silent drop
    });
    await channel.waitForConfirms(); // publisher confirms: broker acked the publish
  }

  async onApplicationShutdown(): Promise<void> {
    await this.channel?.close().catch(() => {});
    await this.connection?.close().catch(() => {});
  }
}
