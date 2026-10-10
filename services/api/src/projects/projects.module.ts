import { Module } from '@nestjs/common';
import { ActionsService } from './actions.service';
import { DeploymentsController } from './deployments.controller';
import { DeploymentsService } from './deployments.service';
import { EnvService } from './env.service';
import { ProjectsController } from './projects.controller';
import { ProjectsService } from './projects.service';

@Module({
  controllers: [ProjectsController, DeploymentsController],
  providers: [ProjectsService, DeploymentsService, ActionsService, EnvService],
  exports: [ActionsService], // admin stop reuses the same service (plan §5)
})
export class ProjectsModule {}
